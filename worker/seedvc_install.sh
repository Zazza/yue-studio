#!/usr/bin/env bash
# Установка Seed-VC для «голоса альбома» (замена тембра голоса) — необязательная
# часть воркера. Всё — код, venv, веса и кэши — в одном каталоге, ничего мимо
# него не пишется: каталог можно перенести на другой диск целиком.
#
# usage: seedvc_install.sh [каталог]
#   каталог по умолчанию — $YUE_SEEDVC_DIR или ~/yue-studio/seedvc;
#   не по умолчанию — пропиши YUE_SEEDVC_DIR=<каталог> в ~/yue-studio/worker.env
# Нужно: git, uv (https://docs.astral.sh/uv/), CUDA-GPU, ~14 ГБ на время установки, ~10 ГБ после.
# Лицензия Seed-VC — GPL-3.0; воркер вызывает его отдельной программой.
set -euo pipefail

DIR="${1:-${YUE_SEEDVC_DIR:-$HOME/yue-studio/seedvc}}"
REPO=https://github.com/Plachtaa/seed-vc.git
COMMIT=51383efd921027683c89e5348211d93ff12ac2a8   # проверенная версия кода
TORCH_INDEX="${YUE_TORCH_INDEX:-https://download.pytorch.org/whl/cu128}"
NEED_GB=14   # на пике: venv ~7.5 (из них CUDA-библиотеки ~4.3) + кэш пакетов ~3.5 + веса ~2.4
HERE="$(cd "$(dirname "$0")" && pwd)"

mkdir -p "$DIR"
DIR="$(cd "$DIR" && pwd)"
free=$(df -Pk "$DIR" | awk 'NR==2 {print int($4 / 1048576)}')
if (( free < NEED_GB )); then
  echo "мало места в $DIR: свободно ${free} ГБ, нужно ~${NEED_GB} ГБ — укажи другой каталог: $0 /путь" >&2
  exit 1
fi
command -v git >/dev/null || { echo "нужен git" >&2; exit 1; }
command -v uv >/dev/null || { echo "нужен uv: https://docs.astral.sh/uv/" >&2; exit 1; }

# кэши uv и Hugging Face — внутри каталога (на одном диске с venv: uv ставит
# жёсткими ссылками, второй копии пакетов нет)
export UV_CACHE_DIR="$DIR/uv-cache" HF_HOME="$DIR/hf"

if [ ! -d "$DIR/code/.git" ]; then
  git clone -q "$REPO" "$DIR/code"
fi
git -C "$DIR/code" fetch -q origin "$COMMIT" 2>/dev/null || true
git -C "$DIR/code" checkout -q "$COMMIT"

uv venv -q --allow-existing --python 3.12 "$DIR/venv"
uv pip install -q --python "$DIR/venv/bin/python" torch==2.10.0 torchaudio==2.10.0 --index-url "$TORCH_INDEX"
uv pip install -q --python "$DIR/venv/bin/python" -r "$HERE/requirements-seedvc.txt"

echo "веса и пробный запуск…"
"$DIR/venv/bin/python" "$HERE/seedvc_run.py" --dir "$DIR" --warmup
# кэш пакетов нужен только для переустановки — после установки не держим
uv cache clean -q 2>/dev/null || true
du -sh "$DIR"
echo "Seed-VC готов: $DIR"
if [ "$DIR" != "$HOME/yue-studio/seedvc" ]; then
  echo "пропиши в ~/yue-studio/worker.env: YUE_SEEDVC_DIR=$DIR  (и перезапусти воркер)"
fi
