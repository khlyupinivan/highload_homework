#!/bin/sh
set -eu

GF_URL="${GF_URL:-http://grafana:3000}"
ADMIN_USER="${GF_ADMIN_USER:-admin}"
ADMIN_PASS="${GF_ADMIN_PASSWORD:-admin}"
SA_NAME="${SA_NAME:-dashboard-gen}"
TOKEN_FILE="${TOKEN_FILE:-/secrets/grafana-token}"

echo "waiting for Grafana ${GF_URL}/api/health ..."
until curl -sf "${GF_URL}/api/health" >/dev/null; do sleep 1; done

AUTH="-u ${ADMIN_USER}:${ADMIN_PASS}"

# 1) Идемпотентно ищем сервис-аккаунт по имени
SA_ID=$(curl -sf $AUTH "${GF_URL}/api/serviceaccounts/search?query=${SA_NAME}" \
  | jq -r --arg n "$SA_NAME" '.serviceAccounts[]? | select(.name==$n) | .id' | head -n1)

# 2) Если нет — создаём (Grafana 12 legacy API)
if [ -z "${SA_ID}" ] || [ "${SA_ID}" = "null" ]; then
  SA_ID=$(curl -sf $AUTH -X POST -H 'Content-Type: application/json' \
    -d "{\"name\":\"${SA_NAME}\",\"role\":\"Editor\",\"isDisabled\":false}" \
    "${GF_URL}/api/serviceaccounts" | jq -r '.id')
  echo "created service account ${SA_NAME} id=${SA_ID}"
else
  echo "reusing service account ${SA_NAME} id=${SA_ID}"
fi

# 3) Токен без срока жизни. Переиспользуем существующий, если уже выдавали.
TOKEN=$(curl -sf $AUTH -X POST -H 'Content-Type: application/json' \
  -d "{\"name\":\"${SA_NAME}-$(date +%s)\",\"secondsToLive\":0}" \
  "${GF_URL}/api/serviceaccounts/${SA_ID}/tokens" | jq -r '.key')

# Если токен уже был — key не отдаётся повторно, поэтому всегда создаём новый с суффиксом времени,
# но старые не удаляем: проще и безопаснее для повторных запусков compose.
if [ -z "${TOKEN}" ] || [ "${TOKEN}" = "null" ]; then
  TOKEN=$(curl -sf $AUTH -X POST -H 'Content-Type: application/json' \
    -d "{\"name\":\"${SA_NAME}-$(date +%s)\",\"secondsToLive\":0}" \
    "${GF_URL}/api/serviceaccounts/${SA_ID}/tokens" | jq -r '.key')
fi

[ -n "${TOKEN}" ] && [ "${TOKEN}" != "null" ] || { echo "failed to obtain SA token" >&2; exit 1; }

mkdir -p "$(dirname "${TOKEN_FILE}")"
printf '%s' "${TOKEN}" > "${TOKEN_FILE}"
echo "SA token written to ${TOKEN_FILE}"