export function blobToBase64(blob) {
  return new Promise((resolve) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).split(',')[1]);
    r.readAsDataURL(blob);
  });
}

export const layerEnd = (l) => l.settings.startTime + l.settings.sourceDuration / Math.abs(l.settings.speed ?? 1);

export function fitDuration(json) {
  return { ...json, duration: Math.max(...json.layers.map(layerEnd)) };
}
