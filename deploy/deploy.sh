#!/usr/bin/env bash
#
# JCP 韭菜盘 —— Web 服务模式一键部署脚本
#
# 用法（在服务器上执行）：
#   bash deploy.sh <服务器地址> [用户名]
#
# 例：
#   bash deploy.sh 120.53.8.253 root
#
# 脚本会：
#   1. 在本地交叉编译 Linux 二进制（避免 2GB 内存机器 OOM）
#   2. 构建前端静态资源
#   3. 上传到服务器
#   4. 安装 systemd 服务并启动
#   5. 配置 Nginx 反向代理（可选）
#
set -euo pipefail

SERVER="${1:-}"
USER_NAME="${2:-root}"
REMOTE_DIR="/opt/jcp"
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -z "$SERVER" ]]; then
    echo "用法: bash deploy.sh <服务器地址> [用户名]"
    exit 1
fi

# ---------- 颜色输出 ----------
info()  { echo -e "\033[0;36m[INFO]\033[0m $*"; }
ok()    { echo -e "\033[0;32m[ OK ]\033[0m $*"; }
warn()  { echo -e "\033[0;33m[WARN]\033[0m $*"; }
fail()  { echo -e "\033[0;31m[FAIL]\033[0m $*"; exit 1; }

# ---------- 前置检查 ----------
command -v go >/dev/null || fail "本机未安装 Go 1.24+"
command -v npm >/dev/null || fail "本机未安装 Node.js 18+"
command -v ssh >/dev/null || fail "本机未安装 ssh"

VERSION=$(git -C "$PROJECT_DIR" describe --tags --always 2>/dev/null || echo "dev")
info "项目版本: $VERSION"

# ---------- 1. 构建前端 ----------
info "构建前端静态资源…"
cd "$PROJECT_DIR/frontend"
[[ -d node_modules ]] || npm install --no-audit --no-fund
npm run build
[[ -d dist ]] || fail "前端构建失败：dist 目录不存在"
ok "前端构建完成"

# ---------- 2. 交叉编译 Linux 二进制 ----------
info "交叉编译 Linux 二进制（CGO_ENABLED=0）…"
cd "$PROJECT_DIR"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -tags web \
    -ldflags "-s -w -X main.WebVersion=${VERSION}" \
    -o "$PROJECT_DIR/jcp-server" . \
    || fail "Go 编译失败"
ok "二进制构建完成: jcp-server ($(du -h "$PROJECT_DIR/jcp-server" | cut -f1))"

# ---------- 3. 上传 ----------
info "上传到 ${USER_NAME}@${SERVER}:${REMOTE_DIR} …"
ssh "${USER_NAME}@${SERVER}" "mkdir -p ${REMOTE_DIR}/data ${REMOTE_DIR}/static"
scp "$PROJECT_DIR/jcp-server" "${USER_NAME}@${SERVER}:${REMOTE_DIR}/jcp-server"
scp -r "$PROJECT_DIR/frontend/dist/." "${USER_NAME}@${SERVER}:${REMOTE_DIR}/static/"
scp "$PROJECT_DIR/deploy/jcp-web.service" "${USER_NAME}@${SERVER}:/tmp/jcp-web.service"
ok "文件上传完成"

# ---------- 4. 安装并启动 ----------
info "安装 systemd 服务并启动…"
ssh "${USER_NAME}@${SERVER}" <<REMOTE
set -e
id jcp >/dev/null 2>&1 || useradd -r -s /sbin/nologin jcp
chmod +x ${REMOTE_DIR}/jcp-server
chown -R jcp:jcp ${REMOTE_DIR}

# 生成访问令牌（已存在则保留）
if [ ! -f /etc/jcp-token ]; then
    openssl rand -hex 32 > /etc/jcp-token
    chmod 600 /etc/jcp-token
fi
TOKEN=\$(cat /etc/jcp-token)

sed "s|CHANGE_ME|\${TOKEN}|" /tmp/jcp-web.service > /etc/systemd/system/jcp-web.service
rm -f /tmp/jcp-web.service

systemctl daemon-reload
systemctl enable jcp-web >/dev/null
systemctl restart jcp-web
sleep 2
systemctl --no-pager --lines=5 status jcp-web || true
REMOTE
ok "服务已启动"

# ---------- 5. 输出访问信息 ----------
TOKEN=$(ssh "${USER_NAME}@${SERVER}" "cat /etc/jcp-token")
echo ""
echo "=============================================="
echo " 部署完成"
echo "=============================================="
echo " 服务地址 : http://${SERVER}:8080"
echo " 访问令牌 : ${TOKEN}"
echo ""
echo " 浏览器访问（含令牌）:"
echo "   http://${SERVER}:8080/?token=${TOKEN}"
echo ""
echo " 查看日志 : ssh ${USER_NAME}@${SERVER} 'journalctl -u jcp-web -f'"
echo " 停止服务 : ssh ${USER_NAME}@${SERVER} 'systemctl stop jcp-web'"
echo "=============================================="
warn "提示：生产环境请在腾讯云控制台配置防火墙，只放行 22/80/443，"
warn "      不要将 8080 端口直接暴露到公网。"
