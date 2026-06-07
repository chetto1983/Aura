# Spike 021 — synth known-text speech probe clips via Windows SAPI TTS.
# Output: D:\tmp\spike-021\probe-en.wav + probe-it.wav (16kHz mono PCM).
# Real speech with KNOWN ground-truth text, zero downloads. WER-grade corpus is spike 022.

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Speech

$outDir = "D:\tmp\spike-021"
New-Item -ItemType Directory -Force $outDir | Out-Null

$synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
$fmt = New-Object System.Speech.AudioFormat.SpeechAudioFormatInfo(16000, [System.Speech.AudioFormat.AudioBitsPerSample]::Sixteen, [System.Speech.AudioFormat.AudioChannel]::Mono)

# Italian voice if installed, else default (accented IT is still a valid acceptance probe)
$itVoice = $synth.GetInstalledVoices() | Where-Object { $_.VoiceInfo.Culture.Name -like "it-*" } | Select-Object -First 1
Write-Host "Installed voices: $(($synth.GetInstalledVoices() | ForEach-Object { $_.VoiceInfo.Name }) -join ', ')"

$enVoice = $synth.GetInstalledVoices() | Where-Object { $_.VoiceInfo.Culture.Name -like "en-*" } | Select-Object -First 1
if ($enVoice) { $synth.SelectVoice($enVoice.VoiceInfo.Name); Write-Host "EN voice: $($enVoice.VoiceInfo.Name)" }
$synth.SetOutputToWaveFile("$outDir\probe-en.wav", $fmt)
$synth.Speak("hello, tell me two plus two")
$synth.SetOutputToNull()

if ($itVoice) { $synth.SelectVoice($itVoice.VoiceInfo.Name); Write-Host "IT voice: $($itVoice.VoiceInfo.Name)" }
else { Write-Host "WARN: no it-* voice installed - probe-it.wav will be EN-accented" }
$synth.SetOutputToWaveFile("$outDir\probe-it.wav", $fmt)
# "ciao, dimmi due piu' due" - accented char built via [char] to keep this file pure ASCII
$synth.Speak(("ciao, dimmi due pi{0} due" -f [char]0x00F9))
$synth.SetOutputToNull()
$synth.Dispose()

Get-ChildItem $outDir\probe-*.wav | Select-Object Name, Length
