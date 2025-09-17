#!/bin/bash

# plus 服务管理脚本
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="${SCRIPT_DIR}/plus.pid"
LOG_DIR="${SCRIPT_DIR}/log"
SERVICE_NAME="plus"

# 创建日志目录
mkdir -p "${LOG_DIR}"

# 启动服务函数
start_service() {
    # 检查服务是否已经在运行
    if [ -f "${PID_FILE}" ]; then
        OLD_PID=$(cat "${PID_FILE}")
        if ps -p "${OLD_PID}" > /dev/null 2>&1; then
            echo "Plus service is already running with PID: ${OLD_PID}"
            return 1
        else
            echo "Removing stale PID file..."
            rm -f "${PID_FILE}"
        fi
    fi

    echo "Starting plus service..."
    nohup plus -s /export \
        --log ./log/plus.log \
        --log-level DEBUG \
        -l :8700 > /dev/null 2>&1 &

    SERVICE_PID=$!
    echo "${SERVICE_PID}" > "${PID_FILE}"

    sleep 2
    if ps -p "${SERVICE_PID}" > /dev/null 2>&1; then
        echo "Plus service started successfully!"
        echo "PID: ${SERVICE_PID}"
        echo "PID file: ${PID_FILE}"
        echo "Log file: ${LOG_DIR}/plus.log"
        return 0
    else
        echo "Failed to start plus service!"
        rm -f "${PID_FILE}"
        return 1
    fi
}

# 停止服务函数
stop_service() {
    if [ ! -f "${PID_FILE}" ]; then
        echo "PID file not found. Service may not be running."
        return 1
    fi

    PID=$(cat "${PID_FILE}")
    if ps -p "${PID}" > /dev/null 2>&1; then
        echo "Stopping plus service (PID: ${PID})..."
        kill "${PID}"
        
        # 等待进程结束
        for i in {1..10}; do
            if ! ps -p "${PID}" > /dev/null 2>&1; then
                break
            fi
            sleep 1
        done
        
        # 如果进程还在运行，强制杀死
        if ps -p "${PID}" > /dev/null 2>&1; then
            echo "Force killing process..."
            kill -9 "${PID}"
        fi
        
        rm -f "${PID_FILE}"
        echo "Plus service stopped."
    else
        echo "Process not found. Removing stale PID file."
        rm -f "${PID_FILE}"
    fi
}

# 检查服务状态函数
status_service() {
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}")
        if ps -p "${PID}" > /dev/null 2>&1; then
            echo "Plus service is running (PID: ${PID})"
            return 0
        else
            echo "Plus service is not running (stale PID file exists)"
            return 1
        fi
    else
        echo "Plus service is not running"
        return 1
    fi
}

# 重启服务函数
restart_service() {
    echo "Restarting plus service..."
    stop_service
    sleep 2
    start_service
}

# 主逻辑
case "${1:-start}" in
    start)
        start_service
        ;;
    stop)
        stop_service
        ;;
    restart)
        restart_service
        ;;
    status)
        status_service
        ;;
    *)
        echo "Usage: \$0 {start|stop|restart|status}"
        echo "Default action is 'start' if no argument provided"
        exit 1
        ;;
esac