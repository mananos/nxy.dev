# Imagen para renderizar los videos de la documentacion con HyperFrames.
#   docker build -t nxy-docs-video:0.9.10 docs/assets/video
# Nada se instala en tu maquina: node_modules vive solo dentro de la imagen (/opt/video).
FROM node:22-bookworm-slim

# Chromium y FFmpeg del sistema (HyperFrames no baja su propio Chrome); fuentes con acentos y emojis.
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium ffmpeg fonts-noto-core fonts-noto-mono fonts-noto-color-emoji ca-certificates \
 && rm -rf /var/lib/apt/lists/*

ENV PUPPETEER_SKIP_DOWNLOAD=1 \
    PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium \
    HYPERFRAMES_BROWSER_PATH=/usr/bin/chromium \
    HYPERFRAMES_NO_TELEMETRY=1 \
    DO_NOT_TRACK=1 \
    PATH=/opt/video/node_modules/.bin:$PATH

WORKDIR /opt/video
COPY package.json package-lock.json ./
RUN npm ci

# El repo se monta en /work y se renderiza desde /work/docs/assets/video.
WORKDIR /work/docs/assets/video
