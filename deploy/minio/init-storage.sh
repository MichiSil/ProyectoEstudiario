#!/bin/sh
# Crea en MinIO el bucket `summaries` y el usuario de forum-service, con
# permiso sólo sobre ese bucket, para que el servicio no use el administrador
# del contenedor (ADR-001, ADR-003). Mismo criterio que
# deploy/postgres/init-databases.sh.
#
# Es idempotente: lo corre el contenedor minio-init en cada
# `docker compose up`.
set -eu

: "${MINIO_ROOT_USER:?falta MINIO_ROOT_USER}"
: "${MINIO_ROOT_PASSWORD:?falta MINIO_ROOT_PASSWORD}"
: "${FORUM_MINIO_ACCESS_KEY:?falta FORUM_MINIO_ACCESS_KEY}"
: "${FORUM_MINIO_SECRET_KEY:?falta FORUM_MINIO_SECRET_KEY}"

BUCKET=summaries
POLICY=forum-summaries

if [ "$FORUM_MINIO_ACCESS_KEY" = "$MINIO_ROOT_USER" ]; then
	echo "FORUM_MINIO_ACCESS_KEY no puede ser el administrador del contenedor." >&2
	exit 1
fi

# mc guarda su configuración en el HOME; en este contenedor se usa /tmp.
export HOME=/tmp
mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null

mc mb --ignore-existing "local/$BUCKET"

# Sólo leer, escribir y borrar objetos del bucket, y listarlo.
cat > /tmp/policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": ["arn:aws:s3:::$BUCKET/*"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket", "s3:GetBucketLocation"],
      "Resource": ["arn:aws:s3:::$BUCKET"]
    }
  ]
}
EOF
mc admin policy create local "$POLICY" /tmp/policy.json >/dev/null

# `user add` también actualiza la contraseña si el usuario ya existe.
mc admin user add local "$FORUM_MINIO_ACCESS_KEY" "$FORUM_MINIO_SECRET_KEY" >/dev/null
# Si la política ya estaba asociada, mc devuelve error: se ignora.
mc admin policy attach local "$POLICY" --user "$FORUM_MINIO_ACCESS_KEY" >/dev/null 2>&1 || true

echo "bucket $BUCKET listo (usuario $FORUM_MINIO_ACCESS_KEY, sólo sobre $BUCKET)"
