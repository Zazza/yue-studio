"""Транскрипция текста песни faster-whisper. Запускается интерпретатором
~/whisper-venv (там стоит faster-whisper; основной venv его не содержит).

usage: whisper_run.py <audio> [--model small] [--language en] → JSON в stdout {text, language}
--language — язык пения (en, ru, …); без него whisper определяет сам и на
миксе с гитарами нередко ошибается (английское пение → «русский» мусор)
"""
import json
import sys


def main() -> None:
    audio = sys.argv[1]
    model_name = "small"
    if "--model" in sys.argv:
        model_name = sys.argv[sys.argv.index("--model") + 1]
    language = None
    if "--language" in sys.argv:
        language = sys.argv[sys.argv.index("--language") + 1] or None
    from faster_whisper import WhisperModel
    model = WhisperModel(model_name, device="cuda", compute_type="float16")
    segments, info = model.transcribe(audio, beam_size=5, language=language)
    text = "\n".join(s.text.strip() for s in segments)
    print(json.dumps({"text": text, "language": info.language}, ensure_ascii=False))


if __name__ == "__main__":
    main()
