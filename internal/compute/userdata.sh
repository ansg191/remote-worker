#!/bin/bash

version="0.1.4"

snap install ffmpeg
snap install go --classic

wget "https://github.com/ansg191/go_encoder/releases/download/v$version/go_encoder_${version}_Linux_x86_64.tar.gz"
tar xvf "go_encoder_${version}_Linux_x86_64.tar.gz"

./go_encoder_worker -p 80 -tmp "/tmp"
