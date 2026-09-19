import { ALL_FORMATS, AudioBufferSource, BlobSource, BufferTarget, EncodedPacketSink, EncodedVideoPacketSource, Input, Mp4OutputFormat, Output } from 'mediabunny';

// Retimed MP4: the video packets are copied with timestamps and durations divided by `speed` (no
// decode, no re-encode — a 2× clip keeps every frame at 48 fps, a 0.5× clip holds each frame twice
// as long), and the already-stretched AudioBuffer is encoded to AAC next to them.
export async function muxRetimed(file, audioBuffer, speed) {
  const input = new Input({ source: new BlobSource(file), formats: ALL_FORMATS });
  const video = await input.getPrimaryVideoTrack();
  const output = new Output({ format: new Mp4OutputFormat(), target: new BufferTarget() });
  const videoSource = new EncodedVideoPacketSource(video.codec);
  const audioSource = new AudioBufferSource({ codec: 'aac', bitrate: 128_000 });
  output.addVideoTrack(videoSource);
  output.addAudioTrack(audioSource);
  await output.start();
  const decoderConfig = await video.getDecoderConfig();
  let first = true;
  for await (const packet of new EncodedPacketSink(video).packets()) {
    await videoSource.add(packet.clone({ timestamp: packet.timestamp / speed, duration: packet.duration / speed }), first ? { decoderConfig } : undefined);
    first = false;
  }
  await audioSource.add(audioBuffer);
  videoSource.close();
  audioSource.close();
  await output.finalize();
  input.dispose?.();
  return new Blob([output.target.buffer], { type: 'video/mp4' });
}
