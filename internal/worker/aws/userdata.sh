#!/bin/bash

version="0.1.9"

snap install ffmpeg
snap install go --classic

mkdir "/data"

mkfs -t xfs /dev/nvme1n1
mount /dev/nvme1n1 /data

wget "https://github.com/ansg191/remote-worker/releases/download/v$version/go_encoder_worker_${version}_Linux_x86_64.tar.gz"
tar xvf "go_encoder_worker_${version}_Linux_x86_64.tar.gz"

cat << EOF | sudo tee /etc/systemd/system/go_encoder_worker.service > /dev/null
[Unit]
Description=go_encoder Worker Service
After=syslog.target network.target
[Service]
User=root
Type=simple

ExecStart=/go_encoder_worker -p 443 -tmp /data
TimeoutStopSec=20
KillMode=process
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF

systemctl -q daemon-reload
systemctl enable --now -q go_encoder_worker
