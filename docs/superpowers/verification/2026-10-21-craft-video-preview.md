# Native FilmCraft video preview

Date: 2026-10-21. The user added video to the preview scope. This probe ran the installed
FilmCraft CLI 0.9.0 in the existing identity box, using an independent project in a unique
`/workspace/scratch/film-preview-<uuid>` directory. No source objects or active editor changed.

Read first: installed `filmcraft-cli --help`, `describe file.newSequence`, `describe file.import`,
the full stored MCP tool inventory and Aura's existing FilmCraft skill/spike findings.

The native CLI already provides the complete path:

1. `probe <source>` reports video dimensions, duration and streams as JSON.
2. `--save-as <project.fcproj> import <source>` persists the imported media and returns its IDs.
3. `--project <project.fcproj> --save exec file.newSequence <JSON>` creates the sequence using
   `fromItem` with the returned ID. The probe did not assume a fixed media ID.
4. `--project <project.fcproj> render --seconds <time> --scale <scale> --out <preview.png>` writes
   a native PNG. No offline-placeholder opt-in was used.

Input: public Sintel trailer MP4, 14,621,544 bytes, H.264 1920x1080 at 24fps with AAC audio.
Duration was 13,261,921,344,000 native ticks (254,016,000,000 ticks per second). The probe selected
`min(5, duration_seconds/10)` and scale `480/max(width,height)`.

Result: an inspected, recognizable **480x270 PNG**, 70,076 bytes, showing the mountain scene.
Probe/import/sequence/render took 1.912 seconds including Docker command overhead, excluding
initial source copy. This is one local observation, not a benchmark or latency guarantee.
The unique scratch directory was removed afterward.

Artifacts: `.superpowers/sdd/2026-10-21-paperless-documents/probe-film-preview.py` and
`output/playwright/paperless-comparison/craft-film-native-preview.png`.

The production adapter must bound native JSON, validate probe dimensions/duration before import,
scope `--data-dir` to the request, retain input/output/time/concurrency limits, validate the PNG,
and propagate native errors rather than accept offline placeholders. Those guards are not proved
by this successful fixture alone. Other codecs/containers still require supported-import checks
and explicit fallbacks. This change does not alter video's metadata-only ingestion.

## Installed browser app

The same MP4 was sent through the deployed FilmCraft iframe bridge. It created a sequence with
video and audio tracks and the expected 52-second duration. Keyboard playback ran for six seconds;
the inspected Program frame showed the moving source scene at 00:00:06:01, with visible audio
meters. The browser console confirmed WebCodecs decoding for AVC and reported no errors.
Only existing GPU preference and egui SetTheme warnings appeared.

Importing a source clip creates a new editable project, so FilmCraft emitted dirty=true after
ready. The existing playback viewer should remain the initial video view, with FilmCraft editing
on request and its unsaved-project guard preserved. The probe saved no project or media object.
Screenshots: `craft-film-browser-import.png` and `craft-film-browser-playback.png` in the same
output directory. This proves this MP4's browser path, not every native-supported codec/container.
