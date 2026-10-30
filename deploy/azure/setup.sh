#!/usr/bin/env bash
# Prepara una VM Ubuntu nueva y levanta Estudiario con HTTPS. Se puede volver
# a correr: no pisa el .env existente y sólo actualiza el código.
#
#   curl -fsSL https://raw.githubusercontent.com/MichiSil/ProyectoEstudiario/main/deploy/azure/setup.sh -o setup.sh
#   bash setup.sh
set -euo pipefail

REPO=https://github.com/MichiSil/ProyectoEstudiario.git
DIR="$HOME/ProyectoEstudiario"

# 1. Swap: la VM tiene 4 GB y compilar las imágenes de Go mientras corren
#    Solr y MongoDB puede quedarse sin memoria.
if ! swapon --show | grep -q /swapfile; then
  echo "==> Creando 4 GB de swap"
  sudo fallocate -l 4G /swapfile
  sudo chmod 600 /swapfile
  sudo mkswap /swapfile >/dev/null
  sudo swapon /swapfile
  echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab >/dev/null
fi

# 2. Docker con el plugin compose.
if ! command -v docker >/dev/null; then
  echo "==> Instalando Docker"
  curl -fsSL https://get.docker.com | sudo sh
  sudo usermod -aG docker "$USER"
fi

# 3. Código.
if [ -d "$DIR/.git" ]; then
  echo "==> Actualizando el código"
  git -C "$DIR" pull --ff-only
else
  echo "==> Clonando el repositorio"
  git clone "$REPO" "$DIR"
fi
cd "$DIR"

# 4. .env de producción: contraseñas y secretos nuevos, al azar. Nunca se
#    usan los valores de desarrollo de .env.example.
if [ ! -f .env ]; then
  read -rp "Dominio de la VM (por ejemplo estudiario.brazilsouth.cloudapp.azure.com): " DOMAIN
  read -rp "Mail del administrador del foro: " ADMIN
  read -rsp "API key de Groq (Enter para usar sólo el motor de reglas): " AI_KEY
  echo
  cp .env.example .env
  for var in POSTGRES_ADMIN_PASSWORD PLANNER_DB_PASSWORD USERS_DB_PASSWORD MONGO_ROOT_PASSWORD \
    FORUM_DB_PASSWORD MINIO_ROOT_PASSWORD FORUM_MINIO_SECRET_KEY JWT_SECRET INTERNAL_TOKEN; do
    sed -i "s|^$var=.*|$var=$(openssl rand -hex 24)|" .env
  done
  sed -i "s|^ADMIN_EMAILS=.*|ADMIN_EMAILS=$ADMIN|" .env
  sed -i "s|^AI_API_KEY=.*|AI_API_KEY=$AI_KEY|" .env
  echo "DOMAIN=$DOMAIN" >>.env
  chmod 600 .env
  echo "==> .env creado con secretos nuevos"
fi

# 5. Levantar todo.
echo "==> Construyendo y levantando (la primera vez tarda unos minutos)"
sudo docker compose -f docker-compose.yml -f deploy/azure/docker-compose.cloud.yml up -d --build
sudo docker compose -f docker-compose.yml -f deploy/azure/docker-compose.cloud.yml ps

echo
echo "Listo: https://$(grep ^DOMAIN= .env | cut -d= -f2)"
