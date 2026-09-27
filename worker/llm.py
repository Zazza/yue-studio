"""Ollama-обёртка воркера: единый chat-вызов.

keep_alive=0 — модель выгружается из VRAM сразу после ответа:
9 ГБ модели рядом с YuE2 (7.7 ГБ) иначе роняют генерацию OOM.
"""
import json
import re
import urllib.request


def ollama_chat(url: str, model: str, system: str, user: str,
                temperature: float = 0.4, timeout: int = 120) -> str:
    """Один non-stream chat-запрос; возвращает content ответа (может быть пустым)."""
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "system", "content": system},
                     {"role": "user", "content": user}],
        "stream": False,
        "keep_alive": 0,
        "options": {"temperature": temperature},
    }).encode()
    r = urllib.request.urlopen(urllib.request.Request(
        url, data=payload, headers={"Content-Type": "application/json"}), timeout=timeout)
    return str(json.load(r).get("message", {}).get("content", "")).strip()


def strip_md(text: str) -> str:
    """Убирает ```-заборы из ответа модели."""
    return re.sub(r"^```[a-z]*\s*|\s*```$", "", text).strip()


def adapt_prompts(text: str, to: str) -> tuple[str, str]:
    """(system, user) для адаптации лирики под пение: перевод с сохранением
    слоговой сетки — иначе вокальная линия кавера поплывёт."""
    system = (
        f"You adapt song lyrics for singing in {to}. Translate meaning and mood, "
        "but PRESERVE the prosody: same number of lines, same line order, "
        "each translated line must have the same syllable count (±1) as the source line, "
        "stressed syllables in the same places. Keep section tags like [Verse]/[Chorus] "
        "and empty lines as-is. Natural singable phrasing, no transliteration. "
        "Output ONLY the adapted lyrics, no comments."
    )
    return system, text.strip()
