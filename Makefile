# 项目名称
BINARY_NAME=senv

# 版本信息
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# 安装目录
PREFIX?=$(HOME)/.local
INSTALL_DIR=$(PREFIX)/bin

# Go 命令
GOCMD=go
GOBUILD=$(GOCMD) build $(LDFLAGS)
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# 主程序路径
MAIN_PATH=./main.go

# 颜色输出
GREEN=\033[0;32m
YELLOW=\033[0;33m
RED=\033[0;31m
NC=\033[0m # No Color

.PHONY: all build clean install uninstall test coverage lint fmt vet help

all: build

# 编译项目
build:
	@echo "$(GREEN)Building $(BINARY_NAME)...$(NC)"
	$(GOBUILD) -o $(BINARY_NAME) $(MAIN_PATH)
	@echo "$(GREEN)Build complete: $(BINARY_NAME)$(NC)"

# 编译所有平台
build-all: build-linux build-darwin build-windows

build-linux:
	@echo "$(GREEN)Building for Linux...$(NC)"
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o bin/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)

build-darwin:
	@echo "$(GREEN)Building for macOS...$(NC)"
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o bin/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)

build-windows:
	@echo "$(GREEN)Building for Windows...$(NC)"
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)

# 清理编译产物
clean:
	@echo "$(YELLOW)Cleaning...$(NC)"
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -rf bin/
	rm -f coverage.out coverage.html
	@echo "$(YELLOW)Clean complete$(NC)"

# 运行测试
test:
	@echo "$(GREEN)Running tests...$(NC)"
	$(GOTEST) -v -race ./...

# 运行测试并生成覆盖率报告
coverage:
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

# 查看覆盖率摘要
coverage-summary:
	@echo "$(GREEN)Coverage summary...$(NC)"
	$(GOTEST) -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -func=coverage.out

# 代码格式化
fmt:
	@echo "$(GREEN)Formatting code...$(NC)"
	$(GOFMT) ./...

# 静态检查
vet:
	@echo "$(GREEN)Running go vet...$(NC)"
	$(GOVET) ./...

# 综合检查（需要安装 golangci-lint）
lint:
	@echo "$(GREEN)Running linter...$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "$(YELLOW)golangci-lint not installed, running go vet instead...$(NC)"; \
		$(GOVET) ./...; \
	fi

# 安装到用户目录
install: build
	@echo "$(GREEN)Installing $(BINARY_NAME) to $(INSTALL_DIR)...$(NC)"
	@mkdir -p $(INSTALL_DIR)
	@cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@chmod +x $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "$(GREEN)Installation complete!$(NC)"
	@echo ""
	@echo "Binary installed to: $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo ""
	@echo "Make sure $(INSTALL_DIR) is in your PATH."
	@echo "Add the following to your shell configuration (~/.bashrc, ~/.zshrc, etc.):"
	@echo "  export PATH=\"\$$PATH:$(INSTALL_DIR)\""

# 卸载
uninstall:
	@echo "$(YELLOW)Uninstalling $(BINARY_NAME) from $(INSTALL_DIR)...$(NC)"
	@rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "$(YELLOW)Uninstallation complete!$(NC)"

# 重新安装
reinstall: uninstall install

# 开发模式（热编译，需要安装 reflex）
dev:
	@if command -v reflex >/dev/null 2>&1; then \
		reflex -g '\.go$$' -s -- sh -c 'make build && ./$(BINARY_NAME)'; \
	else \
		echo "$(RED)reflex not installed. Install with: go install github.com/cespare/reflex@latest$(NC)"; \
	fi

# 检查依赖更新
deps:
	@echo "$(GREEN)Checking dependencies...$(NC)"
	$(GOGET) -u ./...
	$(GOCMD) mod tidy

# 安全检查（需要安装 govulncheck）
security:
	@echo "$(GREEN)Running security check...$(NC)"
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "$(YELLOW)govulncheck not installed. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest$(NC)"; \
	fi

# 运行所有检查
check: fmt vet lint test
	@echo "$(GREEN)All checks passed!$(NC)"

# 发布前准备
release: clean check build-all
	@echo "$(GREEN)Release build complete!$(NC)"
	@ls -la bin/

# 帮助信息
help:
	@echo "$(GREEN)Senv - 安全环境变量管理工具$(NC)"
	@echo ""
	@echo "$(YELLOW)使用方法:$(NC)"
	@echo "  make [target]"
	@echo ""
	@echo "$(YELLOW)可用目标:$(NC)"
	@echo "  $(GREEN)build$(NC)           - 编译项目"
	@echo "  $(GREEN)build-all$(NC)       - 编译所有平台版本"
	@echo "  $(GREEN)clean$(NC)           - 清理编译产物"
	@echo "  $(GREEN)test$(NC)            - 运行测试"
	@echo "  $(GREEN)coverage$(NC)        - 运行测试并生成覆盖率报告"
	@echo "  $(GREEN)coverage-summary$(NC) - 显示覆盖率摘要"
	@echo "  $(GREEN)fmt$(NC)             - 格式化代码"
	@echo "  $(GREEN)vet$(NC)             - 运行 go vet"
	@echo "  $(GREEN)lint$(NC)            - 运行代码检查"
	@echo "  $(GREEN)check$(NC)           - 运行所有检查 (fmt + vet + lint + test)"
	@echo "  $(GREEN)install$(NC)         - 安装到 $(INSTALL_DIR)"
	@echo "  $(GREEN)uninstall$(NC)       - 从 $(INSTALL_DIR) 卸载"
	@echo "  $(GREEN)reinstall$(NC)       - 重新安装"
	@echo "  $(GREEN)deps$(NC)            - 更新依赖"
	@echo "  $(GREEN)security$(NC)        - 运行安全检查"
	@echo "  $(GREEN)dev$(NC)             - 开发模式（热编译）"
	@echo "  $(GREEN)release$(NC)         - 发布前准备"
	@echo "  $(GREEN)help$(NC)            - 显示帮助信息"
	@echo ""
	@echo "$(YELLOW)变量:$(NC)"
	@echo "  VERSION=$(VERSION)"
	@echo "  PREFIX=$(PREFIX)"
	@echo "  INSTALL_DIR=$(INSTALL_DIR)"
