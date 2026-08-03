import type { EdgeLabelDrawingFunction, NodeLabelDrawingFunction } from 'sigma/rendering';

export const ARCADE_NODE_BASE_DIAMETER = 25;
export const ARCADE_NODE_MAX_DIAMETER = 200;
export const ARCADE_LABEL_MAX_LENGTH = 45;

export function studioCaption(caption: string): string {
  const trimmed = caption.trim();
  if (trimmed.length <= ARCADE_LABEL_MAX_LENGTH) return trimmed;
  return `${trimmed.slice(0, ARCADE_LABEL_MAX_LENGTH)}…`;
}

export function studioNodeRadius(label: string, degreeRadius: number): number {
  const diameter = Math.min(ARCADE_NODE_BASE_DIAMETER + 6 * label.length, ARCADE_NODE_MAX_DIAMETER);
  return Math.max(degreeRadius * 1.8, diameter / 2);
}

function relativeLuminance(hex: string): number | undefined {
  const match = /^#([0-9a-f]{6})$/i.exec(hex);
  if (match === null) return undefined;
  const packed = match[1] ?? '';
  const channel = (offset: number) => {
    const value = Number.parseInt(packed.slice(offset, offset + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  };
  const red = channel(0);
  const green = channel(2);
  const blue = channel(4);
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
}

export function contrastTextColor(background: string): '#101114' | '#FFFFFF' {
  const luminance = relativeLuminance(background);
  if (luminance === undefined) return '#FFFFFF';
  const darkContrast = (luminance + 0.05) / 0.05;
  const lightContrast = 1.05 / (luminance + 0.05);
  return darkContrast >= lightContrast ? '#101114' : '#FFFFFF';
}

export function fitCanvasText(
  context: CanvasRenderingContext2D,
  text: string,
  maxWidth: number,
): string {
  if (context.measureText(text).width <= maxWidth) return text;
  if (context.measureText('…').width > maxWidth) return '';
  let fitted = text;
  while (fitted.length > 0 && context.measureText(`${fitted}…`).width > maxWidth) {
    fitted = fitted.slice(0, -1);
  }
  return `${fitted}…`;
}

export function wrapCanvasLabel(
  context: CanvasRenderingContext2D,
  label: string,
  maxWidth: number,
  maxLines: number,
): string[] {
  const words = label.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0 || maxLines <= 0) return [];

  const lines: string[] = [];
  let line = '';
  for (const word of words) {
    const candidate = line === '' ? word : `${line} ${word}`;
    if (context.measureText(candidate).width <= maxWidth) {
      line = candidate;
      continue;
    }
    if (line !== '') lines.push(line);
    line = word;
  }
  if (line !== '') lines.push(line);

  if (lines.length <= maxLines) {
    return lines.map((value) => fitCanvasText(context, value, maxWidth));
  }
  const visible = lines.slice(0, maxLines);
  const last = visible.length - 1;
  visible[last] = fitCanvasText(context, lines.slice(last).join(' '), maxWidth);
  return visible;
}

export const drawStudioNodeLabel: NodeLabelDrawingFunction = (context, data, settings) => {
  if (!data.label) return;
  const fontSize = Math.min(settings.labelSize, Math.max(10, data.size * 0.42));
  const lineHeight = fontSize * 1.08;
  const maxWidth = Math.max(16, data.size * 1.58);
  const maxLines = Math.max(1, Math.min(4, Math.floor((data.size * 1.55) / lineHeight)));

  context.save();
  context.font = `${settings.labelWeight} ${String(fontSize)}px ${settings.labelFont}`;
  context.textAlign = 'center';
  context.textBaseline = 'middle';
  context.fillStyle = contrastTextColor(data.color);
  const lines = wrapCanvasLabel(context, data.label, maxWidth, maxLines);
  const firstY = data.y - ((lines.length - 1) * lineHeight) / 2;
  lines.forEach((line, index) => {
    context.fillText(line, data.x, firstY + index * lineHeight, maxWidth);
  });
  context.restore();
};

export const drawStudioEdgeLabel: EdgeLabelDrawingFunction = (
  context,
  edgeData,
  sourceData,
  targetData,
  settings,
) => {
  if (!edgeData.label) return;
  let sourceX = sourceData.x;
  let sourceY = sourceData.y;
  let targetX = targetData.x;
  let targetY = targetData.y;
  const fullDX = targetX - sourceX;
  const fullDY = targetY - sourceY;
  const fullDistance = Math.hypot(fullDX, fullDY);
  if (fullDistance <= sourceData.size + targetData.size) return;

  sourceX += (fullDX * sourceData.size) / fullDistance;
  sourceY += (fullDY * sourceData.size) / fullDistance;
  targetX -= (fullDX * targetData.size) / fullDistance;
  targetY -= (fullDY * targetData.size) / fullDistance;
  const dx = targetX - sourceX;
  const dy = targetY - sourceY;
  const distance = Math.hypot(dx, dy);
  context.save();
  context.font = `${settings.edgeLabelWeight} ${String(settings.edgeLabelSize)}px ${settings.edgeLabelFont}`;
  const label = fitCanvasText(context, edgeData.label, distance);
  if (label.length < 2) {
    context.restore();
    return;
  }

  let angle = Math.atan2(dy, dx);
  if (angle > Math.PI / 2 || angle < -Math.PI / 2) angle += Math.PI;
  context.translate((sourceX + targetX) / 2, (sourceY + targetY) / 2);
  context.rotate(angle);
  context.textAlign = 'center';
  context.textBaseline = 'bottom';
  context.lineJoin = 'round';
  context.lineWidth = 5;
  context.strokeStyle = '#131314';
  context.fillStyle = settings.edgeLabelColor.color ?? edgeData.color;
  const y = -(edgeData.size + 3);
  context.strokeText(label, 0, y, distance);
  context.fillText(label, 0, y, distance);
  context.restore();
};
