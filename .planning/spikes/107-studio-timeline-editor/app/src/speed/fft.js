// Peak frequency of a mono signal: Hann window over N samples from the middle, radix-2 FFT, parabolic
// interpolation on the log magnitude around the top bin. Also the share of spectral energy within
// ±10 Hz of the peak ("purity"): a clean stretch keeps it near 1, a warbling one smears it.
// Plain ESM, used by the page and by run-speed.mjs in Node.
export function spectrumPeak(samples, sampleRate, N = 32768) {
  const n = Math.min(N, 2 ** Math.floor(Math.log2(samples.length)));
  const start = Math.floor((samples.length - n) / 2);
  const re = new Float64Array(n);
  const im = new Float64Array(n);
  for (let i = 0; i < n; i++) re[i] = samples[start + i] * (0.5 - 0.5 * Math.cos((2 * Math.PI * i) / (n - 1)));
  for (let i = 1, j = 0; i < n; i++) {
    let bit = n >> 1;
    for (; j & bit; bit >>= 1) j ^= bit;
    j ^= bit;
    if (i < j) { [re[i], re[j]] = [re[j], re[i]]; [im[i], im[j]] = [im[j], im[i]]; }
  }
  for (let len = 2; len <= n; len <<= 1) {
    const ang = (-2 * Math.PI) / len;
    for (let i = 0; i < n; i += len) {
      for (let k = 0; k < len / 2; k++) {
        const wr = Math.cos(ang * k), wi = Math.sin(ang * k);
        const ur = re[i + k], ui = im[i + k];
        const vr = re[i + k + len / 2] * wr - im[i + k + len / 2] * wi;
        const vi = re[i + k + len / 2] * wi + im[i + k + len / 2] * wr;
        re[i + k] = ur + vr; im[i + k] = ui + vi;
        re[i + k + len / 2] = ur - vr; im[i + k + len / 2] = ui - vi;
      }
    }
  }
  const binHz = sampleRate / n;
  const power = new Float64Array(n / 2);
  let top = 1;
  for (let k = 1; k < n / 2; k++) {
    power[k] = re[k] ** 2 + im[k] ** 2;
    if (k * binHz > 20 && power[k] > power[top]) top = k;
  }
  const [a, b, c] = [power[top - 1], power[top], power[top + 1]].map((p) => Math.log(p + 1e-30));
  const offset = (a - c) / (2 * (a - 2 * b + c));
  const peakHz = (top + offset) * binHz;
  let total = 0, near = 0;
  for (let k = 1; k < n / 2; k++) {
    if (k * binHz < 20) continue;
    total += power[k];
    if (Math.abs(k * binHz - peakHz) <= 10) near += power[k];
  }
  return { peakHz: +peakHz.toFixed(2), purity: +(near / total).toFixed(4), window: n, binHz: +binHz.toFixed(3) };
}
