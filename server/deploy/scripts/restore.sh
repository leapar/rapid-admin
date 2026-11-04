#!/usr/bin/env bash
# =============================================================================
# restore.sh —— 灾难恢复(从 OSS / 本地备份还原)
# 文档 §11.6
# 使用:
#   ./restore.sh --mysql 20260210-020000          # 还原某次 DB 备份
#   ./restore.sh --all 20260210-020000            # 还原 DB + 上传文件 + 配置
#   ./restore.sh --list                           # 列出可用备份
# =============================================================================
set -euo pipefail

: "${MYSQL_HOST:=127.0.0.1}"
: "${MYSQL_PORT:=3306}"
: "${MYSQL_USER:=rapid-admin}"
: "${MYSQL_PASSWORD:?must be set}"
: "${MYSQL_DB:=rapid-admin}"

: "${BACKUP_DIR:=/var/backups/rapid-admin}"
: "${UPLOAD_DIR:=/var/lib/rapid-admin/uploads}"
: "${CONFIG_DIR:=/etc/rapid-admin}"

: "${OSS_ENDPOINT:=oss-cn-hangzhou.aliyuncs.com}"
: "${OSS_BUCKET:=rapid-admin-backup}"
: "${OSS_ACCESS_KEY:?must be set}"
: "${OSS_ACCESS_SECRET:?must be set}"
: "${OSSCTL_BIN:=ossutil}"

LOG() { echo "[$(date '+%F %T')] $*"; }

usage() {
    cat <<EOF
usage: $0 [--list] [--mysql|--uploads|--config|--all] <ts> [--from-oss]
EOF
    exit 1
}

[[ $# -ge 1 ]] || usage

# 解析参数
LIST_ONLY=0
RESTORE_MYSQL=0
RESTORE_UPLOADS=0
RESTORE_CONFIG=0
FROM_OSS=0
TS=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --list)        LIST_ONLY=1 ;;
        --mysql)       RESTORE_MYSQL=1 ;;
        --uploads)     RESTORE_UPLOADS=1 ;;
        --config)      RESTORE_CONFIG=1 ;;
        --all)         RESTORE_MYSQL=1; RESTORE_UPLOADS=1; RESTORE_CONFIG=1 ;;
        --from-oss)    FROM_OSS=1 ;;
        -*)            usage ;;
        *)             TS="$1" ;;
    esac
    shift
done

# 列出可用备份
if [[ "${LIST_ONLY}" -eq 1 ]]; then
    LOG "available local backups:"
    find "${BACKUP_DIR}" -mindepth 1 -maxdepth 1 -type d | sort
    exit 0
fi

[[ -n "${TS}" ]] || usage

WORKDIR="${BACKUP_DIR}/$(echo "${TS}" | cut -c1-8)"

# 从 OSS 拉取
if [[ "${FROM_OSS}" -eq 1 ]]; then
    LOG ">>> downloading from oss"
    mkdir -p "${WORKDIR}"
    "${OSSCTL_BIN}" cp \
        "oss://${OSS_BUCKET}/$(echo "${TS}" | cut -c1-8)/" \
        "${WORKDIR}/" --recursive \
        --endpoint="${OSS_ENDPOINT}" \
        --access-key-id="${OSS_ACCESS_KEY}" \
        --access-key-secret="${OSS_ACCESS_SECRET}"
fi

# 校验文件
[[ -d "${WORKDIR}" ]] || { LOG "ERROR: backup dir not found: ${WORKDIR}"; exit 1; }
LOG ">>> backup dir: ${WORKDIR} ($(du -sh "${WORKDIR}" | cut -f1))"

# 二次确认
read -rp "This will OVERWRITE current data. Continue? [yes/NO] " ans
[[ "${ans}" == "yes" ]] || { LOG "aborted"; exit 0; }

# ----- 1. MySQL 还原 -----
if [[ "${RESTORE_MYSQL}" -eq 1 ]]; then
    LOG ">>> step 1/n restoring mysql"
    SQL_FILE=$(ls "${WORKDIR}"/mysql-*.sql.gz | head -1)
    [[ -f "${SQL_FILE}" ]] || { LOG "ERROR: mysql backup not found"; exit 1; }
    gunzip -c "${SQL_FILE}" \
        | mysql \
            --host="${MYSQL_HOST}" --port="${MYSQL_PORT}" \
            --user="${MYSQL_USER}" --password="${MYSQL_PASSWORD}" \
            --default-character-set=utf8mb4
    LOG "    mysql restored: ${SQL_FILE}"
fi

# ----- 2. 上传文件 -----
if [[ "${RESTORE_UPLOADS}" -eq 1 ]]; then
    LOG ">>> step 2/n restoring uploads"
    TAR_FILE=$(ls "${WORKDIR}"/uploads-*.tar.gz | head -1)
    if [[ -f "${TAR_FILE}" ]]; then
        mkdir -p "$(dirname "${UPLOAD_DIR}")"
        tar -C "$(dirname "${UPLOAD_DIR}")" -xzf "${TAR_FILE}"
        LOG "    uploads restored: ${TAR_FILE}"
    else
        LOG "    WARN: uploads backup not found, skipped"
    fi
fi

# ----- 3. 配置 -----
if [[ "${RESTORE_CONFIG}" -eq 1 ]]; then
    LOG ">>> step 3/n restoring config"
    TAR_FILE=$(ls "${WORKDIR}"/config-*.tar.gz | head -1)
    if [[ -f "${TAR_FILE}" ]]; then
        tar -C "$(dirname "${CONFIG_DIR}")" -xzf "${TAR_FILE}"
        LOG "    config restored: ${TAR_FILE}"
    fi
fi

LOG ">>> restore done"
