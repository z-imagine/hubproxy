#!/bin/bash
# HubProxy 服务管理脚本
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PID_FILE="${SCRIPT_DIR}/hubproxy.pid"
LOG_FILE="${SCRIPT_DIR}/hubproxy.log"

start() {
    if [ -f "${PID_FILE}" ] && kill -0 $(cat "${PID_FILE}") 2>/dev/null; then
        echo "HubProxy 已在运行 PID=$(cat ${PID_FILE})"
        return 1
    fi
    echo -n "启动 HubProxy..."
    cd "${SCRIPT_DIR}"
    nohup ./hubproxy >> "${LOG_FILE}" 2>&1 &
    echo $! > "${PID_FILE}"
    sleep 2
    if kill -0 $(cat "${PID_FILE}") 2>/dev/null; then
        echo " 成功 PID=$(cat ${PID_FILE})"
    else
        echo " 失败，查看日志: ${LOG_FILE}"
        rm -f "${PID_FILE}"
        exit 1
    fi
}

stop() {
    if [ ! -f "${PID_FILE}" ]; then
        echo "HubProxy 未运行"
        return 0
    fi
    PID=$(cat "${PID_FILE}")
    if ! kill -0 "${PID}" 2>/dev/null; then
        echo "HubProxy 未运行 (PID文件残留，已清理)"
        rm -f "${PID_FILE}"
        return 0
    fi
    echo -n "停止 HubProxy PID=${PID}..."
    kill "${PID}"
    for i in $(seq 1 10); do
        if ! kill -0 "${PID}" 2>/dev/null; then
            echo " 已停止"
            rm -f "${PID_FILE}"
            return 0
        fi
        sleep 1
    done
    echo " 强制终止"
    kill -9 "${PID}" 2>/dev/null || true
    rm -f "${PID_FILE}"
}

restart() {
    stop
    sleep 1
    start
}

status() {
    if [ -f "${PID_FILE}" ] && kill -0 $(cat "${PID_FILE}") 2>/dev/null; then
        PID=$(cat "${PID_FILE}")
        echo "HubProxy 运行中 PID=${PID}"
        echo "启动时间: $(ps -o lstart= -p ${PID} 2>/dev/null || echo '未知')"
        echo ""
        echo "=== 最近日志 ==="
        tail -20 "${LOG_FILE}" 2>/dev/null || echo "(无日志)"
    else
        echo "HubProxy 未运行"
    fi
}

log() {
    if [ -f "${LOG_FILE}" ]; then
        tail -f "${LOG_FILE}"
    else
        echo "日志文件不存在"
    fi
}

case "${1:-status}" in
    start)   start ;;
    stop)    stop ;;
    restart) restart ;;
    status)  status ;;
    log)     log ;;
    *)
        echo "用法: $0 {start|stop|restart|status|log}"
        exit 1
        ;;
esac
