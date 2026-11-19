# Imagen de VHS con git: los tapes de CLI arman un repo de ejemplo con `git init`.
# Se construye una vez: docker build -t nxy-docs-vhs:v0.12.1 docs/assets/tapes
FROM ghcr.io/charmbracelet/vhs:v0.12.1
RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*
