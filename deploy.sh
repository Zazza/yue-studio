#!/usr/bin/env bash
# Yue Studio: сборка desktop-приложения (ПК) и деплой воркера на GPU-машину.
# usage: ./deploy.sh [worker|build]; хост задаётся YUE_DEPLOY_HOST (user@host)
set -euo pipefail
cd "$(dirname "$0")"

HOST="${YUE_DEPLOY_HOST:?задайте YUE_DEPLOY_HOST=user@gpu-host для деплоя воркера}"

CMD="${1:-build}"

if [[ "$CMD" == "worker" ]]; then
  echo "== deploy worker to $HOST =="
  ssh "$HOST" "mkdir -p ~/yue-studio/units ~/yue-studio/data"
  # worker.env — машинные настройки воркера (адрес Ollama и пр.); не перезаписываем
  ssh "$HOST" 'test -f ~/yue-studio/worker.env || cat > ~/yue-studio/worker.env <<EOF
# Настройки воркера (машина-специфичные, в репо не хранится)
# YUE_OLLAMA_URL=http://127.0.0.1:11434/api/chat
# YUE_OLLAMA_MODEL=qwen2.5-chat-ru:latest
EOF'
  scp -q worker/yue_worker.py worker/arc.py worker/plancheck.py worker/dsp.py worker/sheetsage.py worker/stems.py \
        worker/abcparse.py worker/whisper_run.py worker/llm.py worker/media.py worker/waveform.py \
        worker/voice.py worker/seedvc_run.py worker/sep_run.py worker/fx_engine.py worker/drumsynth.py worker/genres.py worker/fx_presets.json worker/fx_nam.py worker/fx_blocks.json worker/presets.py worker/chordgrid.py worker/seedvc_install.sh worker/requirements-seedvc.txt \
        "$HOST:~/yue-studio/"
  scp -q deploy/units/yue-worker.service "$HOST:~/yue-studio/units/"
  ssh "$HOST" '
set -e
export XDG_RUNTIME_DIR=/run/user/$(id -u)
cp ~/yue-studio/units/yue-worker.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user restart yue-worker
sleep 2
systemctl --user --no-pager status yue-worker | head -6
'
  echo "worker OK: http://$(echo "$HOST" | cut -d@ -f2):8091/health"
  exit 0
fi

echo "== build desktop app =="
~/go/bin/wails build
echo "binary: $(pwd)/build/bin/yue-studio"
