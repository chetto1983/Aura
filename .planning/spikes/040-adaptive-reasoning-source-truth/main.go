package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type signal struct {
	Name    string
	Repo    string
	File    string
	Pattern string
	Why     string
}

func main() {
	root := os.Getenv("AURA_ADAPTIVE_REASONING_ROOT")
	if root == "" {
		root = `D:\tmp\aura-spike-040-adaptive-reasoning`
	}

	log("START", "root=%s", root)

	signals := []signal{
		{
			Name:    "adaptthink-rl-method",
			Repo:    "AdaptThink",
			File:    "README.md",
			Pattern: "reinforcement learning (RL) algorithm",
			Why:     "AdaptThink is primarily a training method, not a runtime policy shim.",
		},
		{
			Name:    "adaptthink-heavy-training",
			Repo:    "AdaptThink",
			File:    "README.md",
			Pattern: "H800",
			Why:     "The documented reproduction path is H800/A100 class training.",
		},
		{
			Name:    "adaptthink-vllm-training-dep",
			Repo:    "AdaptThink",
			File:    "requirements.txt",
			Pattern: "vllm==0.8.2",
			Why:     "The released training stack is pinned to vLLM, not Aura's current local sidecars.",
		},
		{
			Name:    "adaptthink-nothinking-loss-knob",
			Repo:    "AdaptThink",
			File:    filepath.Join("scripts", "run_adapt_think_1.5b_deepscaler_16k_delta0.05_btz128_lr2e-6.sh"),
			Pattern: "nothinking_ratio=0.5",
			Why:     "Thinking/NoThinking balance is learned through training parameters.",
		},
		{
			Name:    "adaptthink-logprob-adjustment",
			Repo:    "AdaptThink",
			File:    filepath.Join("src", "adapt_think_core_algos.py"),
			Pattern: "adapt_think_adjust_old_log_prob",
			Why:     "Core behavior touches PPO/log-prob math rather than inference request routing.",
		},
		{
			Name:    "autothink-complexity-classifier",
			Repo:    "optillm",
			File:    filepath.Join("optillm", "autothink", "classifier.py"),
			Pattern: "_fallback_classification",
			Why:     "AutoThink has an inference-time classifier that can be borrowed without model hooks.",
		},
		{
			Name:    "autothink-token-budget",
			Repo:    "optillm",
			File:    filepath.Join("optillm", "autothink", "processor.py"),
			Pattern: "high_complexity_min_tokens",
			Why:     "AutoThink maps complexity to high/low thinking token budgets.",
		},
		{
			Name:    "autothink-direct-model-loop",
			Repo:    "optillm",
			File:    filepath.Join("optillm", "autothink", "processor.py"),
			Pattern: "self.model(input_ids=tokens",
			Why:     "Full AutoThink generation calls a local Transformers model directly.",
		},
		{
			Name:    "autothink-steering-hooks",
			Repo:    "optillm",
			File:    filepath.Join("optillm", "autothink", "steering.py"),
			Pattern: "register_forward_hook",
			Why:     "Activation steering requires PyTorch forward hooks, not a remote OpenAI-compatible API.",
		},
		{
			Name:    "autothink-hf-vector-dataset",
			Repo:    "optillm",
			File:    filepath.Join("optillm", "autothink", "steering.py"),
			Pattern: "datasets.load_dataset",
			Why:     "Steering vectors are external model-specific HF datasets.",
		},
	}

	failures := 0
	for _, s := range signals {
		ok, err := contains(filepath.Join(root, s.Repo, s.File), s.Pattern)
		switch {
		case err != nil:
			failures++
			log("MISS", "%s: %v", s.Name, err)
		case !ok:
			failures++
			log("MISS", "%s: pattern %q not found in %s", s.Name, s.Pattern, filepath.Join(s.Repo, s.File))
		default:
			log("CHECK", "%s: %s", s.Name, s.Why)
		}
	}

	if failures > 0 {
		log("SUMMARY", "PARTIAL: %d required source signals missing", failures)
		os.Exit(1)
	}

	log("FINDING", "AdaptThink = training/RL source truth; useful as prior art and released model family, not as Aura runtime code.")
	log("FINDING", "AutoThink = inference-time source truth; budget classifier is portable, steering/generation require a local PyTorch model process.")
	log("FINDING", "Aura should spike a provider-neutral budget policy separately from activation steering.")
	log("SUMMARY", "VALIDATED: adaptive reasoning names resolved into distinct reusable surfaces")
}

func contains(path string, pattern string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(b), pattern), nil
}

func log(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}
