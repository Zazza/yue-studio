#!/usr/bin/env bash
# Yue Studio: сборка desktop-приложения (ПК) и деплой воркера на 184.
# usage: ./deploy.sh [worker|build]
set -euo pipefail
cd "$(dirname "$0")"

HOST=dsamotoy@192.168.1.184
REMOTE_DIR='$HOME/yue-studio'

CMD="${1:-build}"

if [[ "$CMD" == "worker" ]]; then
  echo "== deploy worker to 184 =="
  ssh "$HOST" "mkdir -p ~/yue-studio/units ~/yue-studio/data"
  scp -q worker/yue_worker.py worker/dsp.py worker/sheetsage.py worker/stems.py \
        worker/abcparse.py worker/whisper_run.py "$HOST:~/yue-studio/"
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
  echo "worker OK: http://192.168.1.184:8091/health"
  exit 0
fi

echo "== build desktop app =="
~/go/bin/wails build
echo "binary: $(pwd)/build/bin/yue-studio"
