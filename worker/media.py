"""Аудио-конвертация воркера (ffmpeg на GPU-машине не предполагается)."""
from pathlib import Path


def encode_mp3(src: Path, dst: Path, bitrate: int = 320) -> None:
    """Кодирует аудиофайл в mp3 через lameenc (чтение — soundfile)."""
    import lameenc
    import soundfile as sf

    data, sr = sf.read(str(src), always_2d=True, dtype="float32")
    inter = (data.T * 32767.0).astype("<i2").tobytes()
    enc = lameenc.Encoder()
    enc.set_bit_rate(bitrate)
    enc.set_in_sample_rate(sr)
    enc.set_channels(data.shape[1])
    enc.set_quality(2)
    dst.write_bytes(enc.encode(inter) + enc.flush())
