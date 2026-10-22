#!/usr/bin/env bash
# docker-build-push.sh —— 构建并推送 wild-work 多架构镜像到阿里云 ACR。
#
# 固化 2026-10-22 两次踩坑的结论（勿回退）：
#   1. **必须 --provenance=false --sbom=false**：BuildKit 默认给镜像附带
#      provenance attestation 清单（application/vnd.oci.empty.v1+json），
#      阿里云 ACR 不认，push 报 `unknown manifest class` 直接失败。
#      Compose/`docker build` 没有对应开关，只能走 `docker buildx build`；
#      BUILDKIT_NO_DEFAULT_ATTESTATIONS=1 实测对 compose build 无效（清单仍在）。
#   2. **GOPROXY 默认 goproxy.cn**：amd64 交叉编译的 `go mod download` 走
#      proxy.golang.org 实测超时（i/o timeout），与 Dockerfile 注释一致；
#      网络可达时可用 GOPROXY=official 切回官方源。
#
# 用法：
#   scripts/docker-build-push.sh                 # 构建 amd64+arm64 并推 latest
#   scripts/docker-build-push.sh v2.9.6          # 同时打 v2.9.6 tag 一起推
#   TAGS_EXTRA=v2.9,edge scripts/docker-build-push.sh   # 额外 tag（逗号分隔）
#   PLATFORMS=linux/amd64 scripts/docker-build-push.sh  # 只推单架构（调试用）
#   SKIP_PUSH=1 scripts/docker-build-push.sh     # 只构建不推送（本地检查清单）
#
# 推送前会校验已 docker login 目标仓库，避免 build 半小时后才报未认证。
set -euo pipefail

IMAGE="${IMAGE:-registry.cn-guangzhou.aliyuncs.com/landwind/wild-work}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
TAG="${1:-latest}"
TAGS_EXTRA="${TAGS_EXTRA:-}"
SKIP_PUSH="${SKIP_PUSH:-0}"
GOPROXY_MODE="${GOPROXY:-goproxy}"   # goproxy（默认 goproxy.cn）| official

cd "$(dirname "$0")/.."

REG_HOST="${IMAGE%%/*}"

# ---- 推送前置校验 ----------------------------------------------------------
if [ "$SKIP_PUSH" != "1" ]; then
  if ! docker manifest inspect "$IMAGE:latest" >/dev/null 2>&1; then
    # 拉取失败也可能是私有仓库未登录；再查一次本地凭据
    if ! grep -qs "$REG_HOST" "${HOME}/.docker/config.json" 2>/dev/null; then
      echo "❌ 未登录 $REG_HOST，先执行：docker login $REG_HOST" >&2
      exit 1
    fi
  fi
fi

# ---- GOPROXY ----------------------------------------------------------------
case "$GOPROXY_MODE" in
  official) BUILD_PROXY="https://proxy.golang.org,direct" ;;
  *)        BUILD_PROXY="https://goproxy.cn,direct" ;;
esac

# ---- 组装 tag 参数 ----------------------------------------------------------
TAG_ARGS=(-t "$IMAGE:$TAG")
if [ -n "$TAGS_EXTRA" ]; then
  IFS=',' read -ra EXTRA <<< "$TAGS_EXTRA"
  for t in "${EXTRA[@]}"; do
    TAG_ARGS+=(-t "$IMAGE:$t")
  done
fi

PUSH_FLAG=(--push)
if [ "$SKIP_PUSH" = "1" ]; then
  PUSH_FLAG=(--load)   # 单架构载入本地；多架构 --load 不可用，故仅单平台时有意义
  [ "${PLATFORMS// /}" != "linux/amd64" ] && [ "${PLATFORMS// /}" != "linux/arm64" ] && {
    echo "⚠️  SKIP_PUSH=1 只支持单平台（--load 限制），当前 PLATFORMS=$PLATFORMS；改为不加载本地" >&2
    PUSH_FLAG=()
  }
fi

echo "▶ image:    $IMAGE:$TAG"
echo "▶ platforms:$PLATFORMS"
echo "▶ goproxy:  $BUILD_PROXY"
echo "▶ push:     $([ "$SKIP_PUSH" = "1" ] && echo no || echo yes)"
echo

# shellcheck disable=SC2086
docker buildx build \
  --platform "$PLATFORMS" \
  --provenance=false --sbom=false \
  --build-arg "GOPROXY=$BUILD_PROXY" \
  "${TAG_ARGS[@]}" \
  "${PUSH_FLAG[@]}" \
  .

echo
echo "✅ 完成。远端清单："
if [ "$SKIP_PUSH" != "1" ]; then
  docker buildx imagetools inspect "$IMAGE:$TAG" | sed -n '1,20p'
fi
