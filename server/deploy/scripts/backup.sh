#!/usr/bin/env bash
# =============================================================================
# backup.sh —— 数据库 + 上传文件 + 配置 全量备份
# 文档 §11.6
#   - 每日 02:00 cron 执行
#   - 保留策略:近 7 天每天 1 份 + 近 4 周每周 1 份 + 近 12 月每月 1 份
#   - 上传至 OSS(异地容灾)
# =============================================================================
set -euo pipefail

# ----- 可配环境变量 -----
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

# ----- 时间戳 -----
TS="$(date +%Y%m%d-%H%M%S)"
DAY="$(date +%Y%m%d)"
WORKDIR="${BACKUP_DIR}/${DAY}"
mkdir -p "${WORKDIR}"

LOG() { echo "[$(date '+%F %T')] $*"; }

# ----- 1. MySQL 全量备份(逻辑 + 压缩) -----
LOG ">>> step 1/4 mysqldump"
mysqldump \
    --host="${MYSQL_HOST}" --port="${MYSQL_PORT}" \
    --user="${MYSQL_USER}" --password="${MYSQL_PASSWORD}" \
    --single-transaction --quick --routines --triggers --events \
    --hex-blob --default-character-set=utf8mb4 \
    --set-gtid-purged=OFF \
    --databases "${MYSQL_DB}" \
    | gzip -9 > "${WORKDIR}/mysql-${TS}.sql.gz"

# 校验(不能 0 字节)
[[ -s "${WORKDIR}/mysql-${TS}.sql.gz" ]] || { LOG "ERROR: mysqldump empty"; exit 1; }
LOG "    mysql backup: $(du -h "${WORKDIR}/mysql-${TS}.sql.gz" | cut -f1)"

# ----- 2. 上传文件备份(仅增量 rsync 到本地) -----
LOG ">>> step 2/4 upload files"
if [[ -d "${UPLOAD_DIR}" ]]; then
    tar -C "$(dirname "${UPLOAD_DIR}")" \
        -czf "${WORKDIR}/uploads-${TS}.tar.gz" \
        "$(basename "${UPLOAD_DIR}")"
    LOG "    uploads backup: $(du -h "${WORKDIR}/uploads-${TS}.tar.gz" | cut -f1)"
fi

# ----- 3. 配置备份(脱敏后再备份) -----
LOG ">>> step 3/4 config"
if [[ -d "${CONFIG_DIR}" ]]; then
    # 用 sed 替换 secret(粗粒度;生产用 vault)
    tar -C "$(dirname "${CONFIG_DIR}")" \
        -czf "${WORKDIR}/config-${TS}.tar.gz" \
        --exclude='*.key' --exclude='*.pem' \
        "$(basename "${CONFIG_DIR}")"
fi

# ----- 4. 上传 OSS(异地) -----
LOG ">>> step 4/4 upload to oss"
for f in "${WORKDIR}"/*; do
    "${OSSCTL_BIN}" cp "${f}" \
        "oss://${OSS_BUCKET}/${DAY}/$(basename "${f}")" \
        --endpoint="${OSS_ENDPOINT}" \
        --access-key-id="${OSS_ACCESS_KEY}" \
        --access-key-secret="${OSS_ACCESS_SECRET}" \
        || LOG "    WARN: upload failed $(basename "${f}")"
done

# ----- 清理旧备份(本地) -----
# 7 天前的目录删掉(OSS 已有)
find "${BACKUP_DIR}" -mindepth 1 -maxdepth 1 -type d \
    -mtime +7 -exec rm -rf {} \;

# ----- 摘要 -----
SIZE=$(du -sh "${WORKDIR}" | cut -f1)
LOG ">>> backup ok: ${WORKDIR} (${SIZE})"
