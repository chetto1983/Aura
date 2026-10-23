"""KV-cached sampling for Kronos (github.com/shiyu-coder/Kronos), which re-encodes its whole window
at every generated step.

Kronos's self-attention is causal with rotary positions counted from the start of the window, so
while the window does not slide a token's keys and values never change and can be kept. Each step
then encodes one token instead of the whole window. Once context + steps exceed the model's maximum
context the original slides the window, re-positions every token and drops the oldest from every
layer's attention: a cache cannot reproduce that, so this refuses to run there.

The dependency-aware layer that samples the second sub-token attends from one query to every final
hidden state; at inference its rotary embedding is sized on that single query, which makes it the
identity, so its keys and values are cached unrotated, as the original computes them.
"""

import numpy as np
import torch
import torch.nn.functional as F


def _rope(rotary, start: int, length: int) -> tuple[torch.Tensor, torch.Tensor]:
    """cos and sin for positions start..start+length-1, computed as RotaryPositionalEmbedding does."""
    t = torch.arange(start, start + length, device=rotary.inv_freq.device).type_as(rotary.inv_freq)
    freqs = torch.einsum("i,j->ij", t, rotary.inv_freq)
    emb = torch.cat((freqs, freqs), dim=-1)
    return emb.cos()[None, None, :, :], emb.sin()[None, None, :, :]


def _heads(proj, x: torch.Tensor, n_heads: int) -> torch.Tensor:
    batch, length, d_model = x.shape
    return proj(x).view(batch, length, n_heads, d_model // n_heads).transpose(1, 2)


def _encode(model, s1, s2, stamp, caches: list[dict], start: int) -> torch.Tensor:
    """Final hidden states of new tokens at positions start.., extending every layer's cache."""
    x = model.embedding([s1, s2]) + model.time_emb(stamp)
    batch, length, d_model = x.shape
    for layer, cache in zip(model.transformer, caches, strict=True):
        attn = layer.self_attn
        h = layer.norm1(x)
        q, k, v = (_heads(p, h, attn.n_heads) for p in (attn.q_proj, attn.k_proj, attn.v_proj))
        cos, sin = _rope(attn.rotary, start, length)
        rotate = attn.rotary._rotate_half
        q, k = q * cos + rotate(q) * sin, k * cos + rotate(k) * sin
        if cache:
            k, v = torch.cat([cache["k"], k], dim=2), torch.cat([cache["v"], v], dim=2)
        cache["k"], cache["v"] = k, v
        out = F.scaled_dot_product_attention(q, k, v, is_causal=start == 0)
        x = x + attn.out_proj(out.transpose(1, 2).contiguous().view(batch, length, d_model))
        x = x + layer.ffn(layer.norm2(x))
    return model.norm(x)


class _DependencyCache:
    def __init__(self, layer):
        self.layer, self.k, self.v = layer, None, None

    def extend(self, hidden: torch.Tensor) -> None:
        attn = self.layer.cross_attn
        k, v = _heads(attn.k_proj, hidden, attn.n_heads), _heads(attn.v_proj, hidden, attn.n_heads)
        self.k = k if self.k is None else torch.cat([self.k, k], dim=2)
        self.v = v if self.v is None else torch.cat([self.v, v], dim=2)

    def last(self, model, hidden_last: torch.Tensor, s1: torch.Tensor) -> torch.Tensor:
        """s2 logits of the last position given its sampled s1 token."""
        attn = self.layer.cross_attn
        q = _heads(attn.q_proj, model.embedding.emb_s1(s1), attn.n_heads)
        out = F.scaled_dot_product_attention(q, self.k, self.v)
        batch, _, d_model = hidden_last.shape
        attn_out = attn.out_proj(out.transpose(1, 2).contiguous().view(batch, 1, d_model))
        return model.head.cond_forward(self.layer.norm(hidden_last + attn_out))[:, -1, :]


def cached_inference(tokenizer, model, x, x_stamp, y_stamp, max_context, pred_len, clip, temperature, top_k, top_p, sample):
    """One sampled path per batch row; same inputs and output as Kronos's auto_regressive_inference
    with sample_count=1. `sample` is Kronos's sample_from_logits."""
    context = x.size(1)
    if context + pred_len > max_context:
        raise ValueError(f"context {context} + {pred_len} steps exceed max_context {max_context}: the window would slide")
    with torch.no_grad():
        s1, s2 = tokenizer.encode(torch.clip(x, -clip, clip), half=True)
        stamp = torch.cat([x_stamp, y_stamp], dim=1)
        caches = [{} for _ in model.transformer]
        hidden = _encode(model, s1, s2, stamp[:, :context], caches, 0)
        dependency = _DependencyCache(model.dep_layer)
        dependency.extend(hidden)
        last = hidden[:, -1:, :]
        generated_s1, generated_s2 = [], []
        for step in range(pred_len):
            pre = sample(model.head(last)[:, -1, :], temperature=temperature, top_k=top_k, top_p=top_p)
            post = sample(dependency.last(model, last, pre), temperature=temperature, top_k=top_k, top_p=top_p)
            generated_s1.append(pre)
            generated_s2.append(post)
            if step < pred_len - 1:
                position = context + step
                last = _encode(model, pre, post, stamp[:, position:position + 1], caches, position)
                dependency.extend(last)
        tokens = [torch.cat([s1, *generated_s1], dim=1), torch.cat([s2, *generated_s2], dim=1)]
        return tokenizer.decode(tokens, half=True).cpu().numpy()


def cached_predictor(module):
    """A KronosPredictor whose generate() uses the cache; preprocessing stays Kronos's own."""

    class CachedPredictor(module.KronosPredictor):
        def generate(self, x, x_stamp, y_stamp, pred_len, T, top_k, top_p, sample_count, verbose):
            if sample_count != 1:
                raise ValueError("draw one sample per batch row")
            tensors = [torch.from_numpy(np.array(a).astype(np.float32)).to(self.device) for a in (x, x_stamp, y_stamp)]
            preds = cached_inference(
                self.tokenizer, self.model, *tensors, self.max_context, pred_len, self.clip, T, top_k, top_p,
                module.sample_from_logits,
            )
            return preds[:, -pred_len:, :]

    return CachedPredictor
