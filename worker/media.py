"""Аудио-конвертация воркера (ffmpeg на GPU-машине не предполагается)."""
from pathlib import Path


def encode_mp3(src: Path, dst: Path, bitrate: int = 320) -> None:
    """Кодирует аудиофайл в mp3 через lameenc (чтение — soundfile)."""
    import lameenc
    import soundfile as sf

    data, sr = sf.read(str(src), always_2d=True, dtype="float32")
    # lameenc ждёт interleaved (L R L R ...); data — (frames, ch).
    # Транспонировать НЕЛЬЗЯ: planar-буфер (L...L R...R) энкодер читает
    # как интерлив — каждый канал ускоряется в 2 раза (баг v0.1.0/v0.1.1).
    inter = (data * 32767.0).astype("<i2").tobytes()
    enc = lameenc.Encoder()
    enc.set_bit_rate(bitrate)
    enc.set_in_sample_rate(sr)
    enc.set_channels(data.shape[1])
    enc.set_quality(2)
    dst.write_bytes(enc.encode(inter) + enc.flush())
