#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${SCRIPT_DIR}"

if [ -f .env ]; then
    source .env
else
    echo "请先创建 .env 文件（参考 .env.example）"
    exit 1
fi

if [ -z "${REMOTE_HOST}" ]; then
    echo "请在 .env 中配置 REMOTE_HOST"
    exit 1
fi

SSH_PORT="${REMOTE_PORT:-22}"
REMOTE_USER="${REMOTE_USER:-root}"
REMOTE_DIR="${REMOTE_DIR:-/opt/hubproxy}"
SSH_OPTS="-p ${SSH_PORT} -o ConnectTimeout=10"
DATE=$(date +%Y%m%d-%H%M%S)

# 1. 编译打包
echo "=== 编译打包 ==="
bash "${SCRIPT_DIR}/build.sh"

PKG=$(ls -t build/hubproxy-*-linux-amd64.tar.gz | head -1)
PKG_NAME=$(basename "${PKG}" .tar.gz)
TMP_PKG="/tmp/${PKG_NAME}.tar.gz"

echo "=== 部署到 ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_DIR} ==="

# 2. 上传
echo "--- 上传 ${PKG_NAME} ---"
scp -P "${SSH_PORT}" "${PKG}" "${REMOTE_USER}@${REMOTE_HOST}:${TMP_PKG}"

# 3. 远程操作
ssh ${SSH_OPTS} "${REMOTE_USER}@${REMOTE_HOST}" bash -s << REMOTE_SCRIPT
set -e

REMOTE_DIR="${REMOTE_DIR}"
DATE="${DATE}"
TMP_PKG="${TMP_PKG}"

# ---- 停止旧进程 ----
if [ -f "\${REMOTE_DIR}/hubproxy.pid" ]; then
    OLD_PID=\$(cat "\${REMOTE_DIR}/hubproxy.pid")
    if kill -0 "\${OLD_PID}" 2>/dev/null; then
        echo "停止旧进程 PID=\${OLD_PID}..."
        kill "\${OLD_PID}"
        sleep 2
        kill -9 "\${OLD_PID}" 2>/dev/null || true
    fi
    rm -f "\${REMOTE_DIR}/hubproxy.pid"
fi

# ---- 备份旧版本 ----
mkdir -p "\${REMOTE_DIR}/backups"
if [ -f "\${REMOTE_DIR}/hubproxy" ]; then
    echo "备份旧版本..."
    mkdir -p "\${REMOTE_DIR}/backups/\${DATE}"
    cp "\${REMOTE_DIR}/hubproxy" "\${REMOTE_DIR}/backups/\${DATE}/hubproxy" 2>/dev/null || true
    cp "\${REMOTE_DIR}/config.toml" "\${REMOTE_DIR}/backups/\${DATE}/config.toml" 2>/dev/null || true
fi

# ---- 保留旧 config ----
if [ -f "\${REMOTE_DIR}/config.toml" ]; then
    cp "\${REMOTE_DIR}/config.toml" /tmp/hubproxy-config-backup.toml
fi

# ---- 解压新版本 ----
echo "解压新版本..."
mkdir -p "\${REMOTE_DIR}"
tar xzf "\${TMP_PKG}" -C "\${REMOTE_DIR}" --strip-components=1
rm -f "\${TMP_PKG}"

# ---- 恢复旧 config（不覆盖已有的远程配置）----
if [ -f /tmp/hubproxy-config-backup.toml ]; then
    cp /tmp/hubproxy-config-backup.toml "\${REMOTE_DIR}/config.toml"
    rm -f /tmp/hubproxy-config-backup.toml
    echo "已恢复远程配置文件"
fi

# ---- 启动新进程 ----
echo "启动 HubProxy..."
cd "\${REMOTE_DIR}"
nohup ./hubproxy > hubproxy.log 2>&1 &
echo \$! > hubproxy.pid
sleep 2

if kill -0 \$(cat hubproxy.pid) 2>/dev/null; then
    echo "启动成功 PID=\$(cat hubproxy.pid)"
else
    echo "启动失败，查看日志: \${REMOTE_DIR}/hubproxy.log"
    cat hubproxy.log
    exit 1
fi
REMOTE_SCRIPT

echo ""
echo "=== 部署完成 ==="
echo "日志: ssh -p ${SSH_PORT} ${REMOTE_USER}@${REMOTE_HOST} 'tail -f ${REMOTE_DIR}/hubproxy.log'"
echo "回滚: ssh -p ${SSH_PORT} ${REMOTE_USER}@${REMOTE_HOST} 'cp ${REMOTE_DIR}/backups/${DATE}/* ${REMOTE_DIR}/'"
