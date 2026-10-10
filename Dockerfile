# wild-work 容器镜像
#
# 设计约束（详见 AGENTS.md R44）：
#   - 容器无桌面 → 恒以 --no-tray 无头模式运行（R12）；
#   - 签到调度/临期统计用本地时区（time.Now()/time.Local），必须带 tzdata 且默认
#     TZ=Asia/Shanghai——默认 UTC 会让 09:00 签到漂移到 17:00；
#   - WILDWORK_HOME=/data 是唯一数据落点（config.json + auths/ + data/），
#     布局与便携版安装目录 1:1 一致，宿主机整个安装目录可直接挂进来迁移；
#   - 端口映射要求进程监听 0.0.0.0，而非环回监听强制要求管理密码（R4），
#     故 compose / 运行文档强制引导设置 WILDWORK_ADMIN_PASSWORD。

########## 构建阶段 ##########
# --platform=$BUILDPLATFORM：构建阶段固定跑在构建机平台，用 GOOS/GOARCH 交叉编译出
# 目标平台二进制——避免整个 go build 跑在 QEMU 模拟下（arm64 慢一个数量级）。
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
# 模块代理：默认官方源；官方源不可达的网络（如中国大陆）传
# --build-arg GOPROXY=https://goproxy.cn,direct（docker-compose.yml 里有注释示例）
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src

# 依赖层先行：go.mod/go.sum 未变时跳过 go mod download
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# 静态链接（R7/R11：CGO_ENABLED=0，纯 Go 无 cgo 依赖）。
# -trimpath 去掉本机路径；-s -w 去符号表/DWARF 减体积（panic 栈仍可读，pclntab 保留）。
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w" -o /out/wild-work ./cmd/wild-work

########## 运行阶段 ##########
FROM alpine:3.22

# ca-certificates：上游全 HTTPS，缺了所有渠道请求都会 x509 失败
# tzdata：配合 ENV TZ（签到时刻按此时区计算）
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 1000 wildwork \
 && adduser -D -u 1000 -G wildwork wildwork \
 && mkdir -p /data && chown wildwork:wildwork /data

COPY --from=build /out/wild-work /usr/local/bin/wild-work

# TZ：签到调度时区（镜像默认上海，可 -e TZ=... 覆盖）
# WILDWORK_HOME：数据根目录（main.workDir() 优先读它），改动需同步 VOLUME 与挂载点
# WILDWORK_CALLBACK_LISTEN：OAuth 本机回调（traework/qwenwork）——容器内必须绑
# 0.0.0.0 且固定端口，运行时需把该端口发布到宿主（docker run -p 127.0.0.1:43903:43903
# 或 compose 已配好），否则浏览器拿 code 后 connection refused（见 internal/logincb）。
ENV TZ=Asia/Shanghai \
    WILDWORK_HOME=/data \
    WILDWORK_CALLBACK_LISTEN=0.0.0.0:43903
WORKDIR /data
VOLUME ["/data"]
EXPOSE 7863
# OAuth 本机回调端口（与 WILDWORK_CALLBACK_LISTEN 一致；发布方式见上）
EXPOSE 43903

# 非 root（uid 1000）运行：/data 挂载点需对 uid 1000 可写——Linux 主机
# `mkdir -p wildwork-data && sudo chown 1000:1000 wildwork-data`，
# Docker Desktop（macOS/Windows）自动映射无需处理；
# 也可 docker run --user "$(id -u):$(id -g)" 覆盖。
USER wildwork

# /healthz 无鉴权（server.Handler 注册，不走 withAuth）。
# 改过监听端口时健康检查需同步改（compose 里可覆盖 healthcheck）。
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:7863/healthz" || exit 1

# ENTRYPOINT 固定二进制；覆盖 CMD 传其它参数时必须自带 --no-tray（容器内无托盘，
# 不带会因 DBus 初始化 panic 而退出——见 R12）。
ENTRYPOINT ["/usr/local/bin/wild-work"]
CMD ["--no-tray"]
