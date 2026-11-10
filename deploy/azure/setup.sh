#!/usr/bin/env bash
# Prepara una VM Ubuntu nueva y levanta Estudiario con HTTPS. Se puede volver
# a correr: actualiza el código, conserva el .env y sólo le agrega lo que falte.
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
C="sudo docker compose -f docker-compose.yml -f deploy/azure/docker-compose.cloud.yml"
nuevo() { openssl rand -hex 24; }
valor() { grep "^$1=" "${2:-.env}" | tail -1 | cut -d= -f2-; }
poner() {
  sed -i "/^$1=/d" .env
  printf '%s=%s
' "$1" "$2" >>.env
}

if [ ! -f .env ]; then
  read -rp "Dominio de la VM (por ejemplo estudiario.brazilsouth.cloudapp.azure.com): " DOMAIN
  read -rp "Mail del administrador del foro: " ADMIN
  read -rsp "API key de Groq (Enter para usar sólo el motor de reglas): " AI_KEY
  echo
  cp .env.example .env
  chmod 600 .env
  # Administradores de cada contenedor: sólo se aplican al crear el volumen.
  for var in POSTGRES_ADMIN_PASSWORD MONGO_ROOT_PASSWORD MINIO_ROOT_PASSWORD RABBITMQ_ADMIN_PASSWORD; do
    poner "$var" "$(nuevo)"
  done
  poner ADMIN_EMAILS "$ADMIN"
  poner AI_API_KEY "$AI_KEY"
  poner DOMAIN "$DOMAIN"
  echo "==> .env creado"
fi

# 4b. Variables que se sumaron al .env.example después de crear el .env (por
#     ejemplo en una VM que ya estaba andando): se copian las que falten.
while IFS= read -r linea; do
  linea=${linea%$'\r'}
  case "$linea" in '' | \#*) continue ;; esac
  grep -q "^${linea%%=*}=" .env || { echo "$linea" >>.env; echo "  agregada ${linea%%=*}"; }
done <.env.example

# 4c. Secretos de cada servicio que sigan con el valor de desarrollo: se cambian
#     por uno al azar. db-init, mongo-init, minio-init y rabbitmq-init actualizan
#     la contraseña del usuario en cada arranque, así que sirve también con datos.
for var in PLANNER_DB_PASSWORD USERS_DB_PASSWORD FORUM_DB_PASSWORD NOTIFICATION_DB_PASSWORD \
  FORUM_MINIO_SECRET_KEY FORUM_RABBITMQ_PASSWORD NOTIFICATION_RABBITMQ_PASSWORD \
  USERS_RABBITMQ_PASSWORD JWT_SECRET INTERNAL_TOKEN GRAFANA_ADMIN_PASSWORD; do
  if [ "$(valor "$var")" = "$(valor "$var" .env.example)" ]; then
    poner "$var" "$(nuevo)"
    echo "  $var: valor nuevo"
  fi
done

# El administrador de RabbitMQ sólo se crea con el volumen: si ya existe y sigue
# con la contraseña de desarrollo, se la cambia adentro del broker.
if [ "$(valor RABBITMQ_ADMIN_PASSWORD)" = "$(valor RABBITMQ_ADMIN_PASSWORD .env.example)" ]; then
  NUEVA=$(nuevo)
  if [ -n "$($C ps -q --status running rabbitmq 2>/dev/null)" ]; then
    $C exec -T rabbitmq rabbitmqctl change_password "$(valor RABBITMQ_ADMIN_USER)" "$NUEVA" >/dev/null
    poner RABBITMQ_ADMIN_PASSWORD "$NUEVA"
    echo "  RABBITMQ_ADMIN_PASSWORD: valor nuevo"
  elif [ -z "$(sudo docker volume ls -q --filter name=rabbitmq-data)" ]; then
    poner RABBITMQ_ADMIN_PASSWORD "$NUEVA"
  else
    echo "  AVISO: RABBITMQ_ADMIN_PASSWORD sigue con el valor de desarrollo (levantá rabbitmq y volvé a correr el script)"
  fi
fi

# Dirección pública que va en el mail de los autores de la agencia.
if [ "$(valor APP_URL)" = "$(valor APP_URL .env.example)" ]; then
  poner APP_URL "https://$(valor DOMAIN)"
fi

# API key del Grupo 4 (ADR-014). La de .env.example es la de prueba local, que
# está publicada en el repositorio: en la nube se genera una real. La clave se
# guarda en ~/clave-grupo4.txt para entregarla por privado; en el .env va sólo
# su hash. Si quedó con el nombre viejo del consumidor (grupo4), se renombra y
# la clave que ya tienen sigue andando.
HASH=$(valor FORUM_API_KEYS | cut -d: -f2)
if [ "$HASH" = "$(valor FORUM_API_KEYS .env.example | cut -d: -f2)" ]; then
  CLAVE=$(nuevo)
  poner FORUM_API_KEYS "agencia_de_viajes:$(printf '%s' "$CLAVE" | sha256sum | cut -d' ' -f1)"
  (umask 077 && echo "$CLAVE" >"$HOME/clave-grupo4.txt")
  echo "  FORUM_API_KEYS: clave nueva del Grupo 4 en ~/clave-grupo4.txt (pasala por privado y borrá el archivo)"
elif valor FORUM_API_KEYS | grep -q '^grupo4:'; then
  poner FORUM_API_KEYS "$(valor FORUM_API_KEYS | sed 's/^grupo4:/agencia_de_viajes:/')"
  echo "  FORUM_API_KEYS: consumidor renombrado a agencia_de_viajes"
fi

# API key del Club de Beneficios del Grupo 1 (ADR-009): la entrega el Grupo 1.
if [ -z "$(valor CLUB_API_KEY)" ]; then
  read -rsp "API key del Club de Beneficios (Enter para dejarla vacía): " CLUB_KEY
  echo
  [ -n "$CLUB_KEY" ] && poner CLUB_API_KEY "$CLUB_KEY"
fi
chmod 600 .env

# 5. Levantar todo.
echo "==> Construyendo y levantando (la primera vez tarda unos minutos)"
$C up -d --build
$C ps

echo
echo "Listo: https://$(grep ^DOMAIN= .env | cut -d= -f2)"
