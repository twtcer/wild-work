package main

// 本测试锁定「桌面默认参数 → docker-compose .env 外部配置」这条链路的**接线**：
//
//	internal/config.applyEnv 里的每个 WILDWORK_* 环境变量
//	    ↓ 必须在 docker-compose.yml 的 environment: 里透传（否则 .env 写了也不进容器）
//	    ↓ 必须在 .env.example 里有一行默认值（否则用户无从得知有哪些旋钮）
//	    ↓ 标量项的默认值必须与 internal/config.Default() 一致（否则「同步默认」是假的）
//
// 与 R34（漏 go xxxSch.Run()）、R41（漏解析回调 flag）、docker_callback_test（漏发布
// 回调端口）同族：**声明了却没接线**——不报错不崩溃，只是 .env 里的配置静默无效。
//
// 容器固定项（刻意不走 .env，改了要同步 ports/健康检查）：
//	WILDWORK_LISTEN / WILDWORK_CALLBACK_LISTEN —— compose 写死 0.0.0.0 + 固定端口；
//	WILDWORK_HOME —— Dockerfile 写死 /data（数据落点，VOLUME 挂载点）。

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"wild-work/internal/config"
)

// envVarsInApplyEnv 从 internal/config/config.go 收集 applyEnv 认识的环境变量。
func envVarsInApplyEnv(t *testing.T) []string {
	t.Helper()
	src := readRepoFile(t, "internal/config/config.go")
	ms := regexp.MustCompile(`os\.Getenv\("(WILDWORK_[A-Z0-9_]+)"\)`).FindAllStringSubmatch(src, -1)
	if len(ms) == 0 {
		t.Fatal("config.go 里没找到 os.Getenv(\"WILDWORK_*\") —— 解析正则失效？")
	}
	var out []string
	for _, m := range ms {
		out = append(out, m[1])
	}
	return out
}

// composeEnvValue 取 compose environment: 里某个变量的字面值。
func composeEnvValue(t *testing.T, compose, key string) (string, bool) {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s*` + key + `:\s*(.+)$`).FindStringSubmatch(compose)
	if m == nil {
		return "", false
	}
	return strings.Trim(m[1], `"`), true
}

// envExampleValues 解析 .env.example 的 KEY=value（忽略整行注释与行尾 # 说明）。
func envExampleValues(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(readRepoFile(t, ".env.example"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if i := strings.Index(v, "#"); i >= 0 {
			v = v[:i]
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func TestDockerEnvPassthroughIsWired(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yml")
	example := envExampleValues(t)
	cfg := config.Default()

	// 容器固定项：刻意不走 .env（写死在 compose）
	fixed := map[string]string{
		"WILDWORK_LISTEN":          "容器内必须 0.0.0.0 + 固定端口（ports/健康检查依赖）",
		"WILDWORK_CALLBACK_LISTEN": "OAuth 回调必须 0.0.0.0 + 固定端口（宿主 127.0.0.1:43903 转发依赖）",
	}

	// 带兜底/强制语义的透传项（不能用空兜底 ${VAR:-}）：
	//   ADMIN_PASSWORD 用 ${VAR:?} 强制（R4）；API_KEY 带公开兜底默认值（见下方 ⑤）
	special := map[string]bool{"WILDWORK_ADMIN_PASSWORD": true, "WILDWORK_API_KEY": true}

	for _, key := range envVarsInApplyEnv(t) {
		if special[key] {
			continue
		}
		if why, ok := fixed[key]; ok {
			val, found := composeEnvValue(t, compose, key)
			if !found {
				t.Errorf("容器固定项 %s 没在 docker-compose.yml 里出现（%s）", key, why)
				continue
			}
			if strings.Contains(val, "${") {
				t.Errorf("%s 必须是固定值（%s），现在是可透传的 %q —— .env 一改就破坏端口映射", key, why, val)
			}
			continue
		}

		// ① compose 必须透传，且用 ${VAR:-} 形式（不写 = 不注入 = 不盖掉 config.json）
		want := key + `: "${` + key + `:-}"`
		if !strings.Contains(compose, want) {
			if val, found := composeEnvValue(t, compose, key); found {
				t.Errorf("docker-compose.yml 的 %s 应为 %q，实际 %q —— 必须 ${%s:-} 形式：\n"+
					"空值会被 applyEnv 跳过（面板/config.json 仍生效），非空才覆盖", key, want, val, key)
			} else {
				t.Errorf("docker-compose.yml 缺少 %s 的透传 —— .env 里写了也进不了容器（applyEnv 认识它）", key)
			}
			continue
		}

		// ② .env.example 必须给出这一项（否则用户不知道有这个旋钮）
		if _, ok := example[key]; !ok {
			t.Errorf(".env.example 缺少 %s= 行 —— 「桌面默认参数都同步到 .env」缺一项", key)
		}
	}

	// 容器数据落点固定在 /data（Dockerfile），与 .env.example 的相对路径配套
	dockerfile := readRepoFile(t, "Dockerfile")
	if !regexp.MustCompile(`WILDWORK_HOME=/data`).MatchString(dockerfile) {
		t.Error("Dockerfile 未设 WILDWORK_HOME=/data —— .env.example 里的相对路径会落到别处")
	}

	// ③ 管理密码必须强制（R4：非环回监听无密码 = 局域网任意设备可退出程序）
	if v, ok := composeEnvValue(t, compose, "WILDWORK_ADMIN_PASSWORD"); !ok || !strings.Contains(v, "${WILDWORK_ADMIN_PASSWORD:?") {
		t.Errorf("WILDWORK_ADMIN_PASSWORD 必须用 ${VAR:?} 强制设置，实际 %q（R4）", v)
	}

	// ④ 标量默认值必须与桌面形态 config.Default() 一致（「同步默认」的实义）
	for env, want := range map[string]string{
		"WILDWORK_AUTH_DIR":        cfg.AuthDir,
		"WILDWORK_STATE_FILE":      cfg.StateFile,
		"WILDWORK_REGION":          cfg.Region,
		"WILDWORK_HARD_CREDIT":     cfg.Cooldown.HardCredit,
		"WILDWORK_SOFT_RATE":       cfg.Cooldown.SoftRate,
		"WILDWORK_ERR_THRESHOLD":   strconv.Itoa(cfg.Cooldown.ErrThresh),
		"WILDWORK_ERR_COOLDOWN":    cfg.Cooldown.ErrCooldown,
		"WILDWORK_TIMEOUT_SECONDS": strconv.Itoa(cfg.Upstream.TimeoutSeconds),
		"WILDWORK_CALLBACK_HOST":   cfg.Callback.Host,
		"WILDWORK_DEFAULT_CHANNEL": cfg.Compat.DefaultChannel,
		"WILDWORK_MAX_TOKENS_CAP":  strconv.Itoa(cfg.Compat.MaxTokensCap),
	} {
		v, ok := example[env]
		if !ok {
			t.Errorf(".env.example 缺 %s", env)
			continue
		}
		if v != want {
			t.Errorf(".env.example 的 %s=%q 与 config.Default() 的 %q 不一致 —— "+
				"改了 config.Default() 就要同步 .env.example（同 AGENTS.md 不变量 7）", env, v, want)
		}
	}

	// ⑤ 接口 Key：容器兜底默认与 .env.example 一致（它是唯一带兜底值的透传项）
	v, ok := composeEnvValue(t, compose, "WILDWORK_API_KEY")
	if !ok || !strings.Contains(v, "${WILDWORK_API_KEY:-") {
		t.Errorf("WILDWORK_API_KEY 必须是 ${WILDWORK_API_KEY:-<默认>} 形式，实际 %q", v)
	} else {
		fallback := strings.TrimSuffix(strings.TrimPrefix(v, "${WILDWORK_API_KEY:-"), "}")
		if ex := example["WILDWORK_API_KEY"]; ex != "" && ex != fallback {
			t.Errorf(".env.example 的 WILDWORK_API_KEY=%q 与 compose 兜底值 %q 不一致", ex, fallback)
		}
	}

	// ⑥ README 得让用户知道有 .env.example 可拷
	if readme := readRepoFile(t, "README.md"); !strings.Contains(readme, ".env.example") {
		t.Error("README.md 未提到 .env.example —— 新用户不知道该 cp 一份")
	}
}
