"""Транскрипция текста песни faster-whisper. Запускается интерпретатором
~/whisper-venv (там стоит faster-whisper; основной venv его не содержит).

usage: whisper_run.py <audio> [--model small] → JSON в stdout {text, language}
"""
import json
import sys


def main() -> None:
    audio = sys.argv[1]
    model_name = "small"
    if "--model" in sys.argv:
        model_name = sys.argv[sys.argv.index("--model") + 1]
    from faster_whisper import WhisperModel
    model = WhisperModel(model_name, device="cuda", compute_type="float16")
    segments, info = model.transcribe(audio, beam_size=5)
    text = "\n".join(s.text.strip() for s in segments)
    print(json.dumps({"text": text, "language": info.language}, ensure_ascii=False))


if __name__ == "__main__":
    main()
