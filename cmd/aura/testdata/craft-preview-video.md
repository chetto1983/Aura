# Synthetic Craft preview video

`craft-preview.mp4` is a 320x180 H.264 clip with eight gradient frames and no audio,
generated on 2026-10-21 using FilmCraft CLI 0.9.0. It contains no third-party footage.

A generated RGB-gradient PNG was imported into a fresh native FilmCraft project and
placed in a sequence. Native `file.exportMedia` exported the custom range 0–0.25 seconds
as H.264, with `bitrateMode:"crf"`, `crf:23` and `wait:true`. The fixture exercises
probe/import/sequence/frame preview through the installed native engine in CI.
