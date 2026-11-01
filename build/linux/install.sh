#!/bin/bash
# 串口调试工具 —— 用户态安装脚本
#
# 设计要点：
#   - 装到 $HOME 下，不碰 /usr（这个软件的 settings/、logs/、history/ 都写在
#     exe 旁边，装到只读目录会全部失效，所以必须是可写目录）
#   - 除「串口权限」外全程无需 root；串口权限走 pkexec，弹图形密码框
#   - 幂等：重复执行等于升级
#
# 用法：
#   ./install.sh                安装（会询问是否顺带修复串口权限）
#   ./install.sh --yes          安装且不询问（默认不碰 udev）
#   ./install.sh --fix-permissions   只修串口权限
#   ./install.sh --uninstall    卸载
#   ./install.sh --help

set -u

APP_NAME="serial-tool"
PREFIX="${SERIAL_TOOL_PREFIX:-$HOME/.local/lib/$APP_NAME}"
BIN_DIR="$PREFIX/bin"

APPS_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
LOCAL_BIN="$HOME/.local/bin"

DESKTOP_FILE="$APPS_DIR/$APP_NAME.desktop"
UNIT_FILE="$UNIT_DIR/$APP_NAME.service"
ICON_FILE="$ICON_DIR/$APP_NAME.png"
UDEV_RULE="/etc/udev/rules.d/60-$APP_NAME.rules"

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

say()  { printf '%s\n' "$*"; }
warn() { printf '  ! %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

print_help() {
    sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

# ---------- 运行库检查 ----------
# GUI 是 GTK4 + WebKitGTK 6.0，缺了连窗口都开不出来，所以安装时先说清楚。
check_runtime_deps() {
    local missing=()
    command -v pkg-config >/dev/null 2>&1 || missing+=("pkg-config")
    if command -v pkg-config >/dev/null 2>&1; then
        pkg-config --exists gtk4        || missing+=("libgtk-4-1")
        pkg-config --exists webkitgtk-6.0 || missing+=("libwebkitgtk-6.0-4")
        pkg-config --exists libsoup-3.0   || missing+=("libsoup-3.0-0")
    fi
    if [ ${#missing[@]} -gt 0 ]; then
        warn "缺少运行时依赖: ${missing[*]}"
        say  "    请先安装（需要管理员密码）："
        say  "      sudo apt install -y libgtk-4-1 libwebkitgtk-6.0-4 libsoup-3.0-0"
        say  "    本工具只支持 GTK4 + WebKitGTK 6.0 的系统（Ubuntu 24.04 及以上）。"
        return 1
    fi
    return 0
}

# ---------- 串口权限 ----------
# uaccess：由 systemd-logind 在设备出现/用户登录时下发 ACL，
# 效果是「不用加组、不用注销、插拔即生效」。
write_udev_rule() {
    local target="$1"
    cat > "$target" <<'EOF'
# 串口调试工具：让当前登录用户直接读写 USB 串口（uaccess 由 systemd-logind 下发 ACL）
KERNEL=="ttyUSB[0-9]*", SUBSYSTEM=="tty", TAG+="uaccess"
KERNEL=="ttyACM[0-9]*", SUBSYSTEM=="tty", TAG+="uaccess"
EOF
}

fix_permissions() {
    local tmp
    tmp="$(mktemp)"
    write_udev_rule "$tmp"

    if [ "$(id -u)" -eq 0 ]; then
        install -m 0644 "$tmp" "$UDEV_RULE" || { rm -f "$tmp"; return 1; }
        udevadm control --reload-rules && udevadm trigger --subsystem-match=tty
        rm -f "$tmp"
        say "  串口权限规则已安装：$UDEV_RULE"
        return 0
    fi

    if command -v pkexec >/dev/null 2>&1; then
        say "  即将弹出授权窗口，用于写入 $UDEV_RULE"
        if pkexec /bin/sh -c 'install -m 0644 "$1" "$2" && udevadm control --reload-rules && udevadm trigger --subsystem-match=tty' \
                _ "$tmp" "$UDEV_RULE"; then
            rm -f "$tmp"
            say "  串口权限规则已安装，无需注销，重新插拔设备即可生效。"
            return 0
        fi
        rm -f "$tmp"
        warn "授权被取消或失败"
        return 1
    fi

    rm -f "$tmp"
    warn "没有 pkexec，请手工执行下面的命令："
    say "    sudo install -m 0644 <规则文件> $UDEV_RULE && sudo udevadm control --reload-rules && sudo udevadm trigger"
    return 1
}

need_permission_fix() {
    # 已经装过规则就不用再问
    [ -f "$UDEV_RULE" ] && return 1
    # 有串口而且当前用户读写不了，才值得提示
    local dev
    dev="$(ls /dev/ttyUSB* /dev/ttyACM* 2>/dev/null | head -n1)"
    [ -n "$dev" ] || return 1
    [ -r "$dev" ] && [ -w "$dev" ] && return 1
    return 0
}

# ---------- 安装 ----------
do_install() {
    local assume_yes="$1"

    [ -x "$SRC_DIR/bin/serial-gui" ] || die "找不到 bin/serial-gui，请从完整安装包里运行本脚本"

    check_runtime_deps || say "  （仍会安装，但界面可能无法启动）"

    say "安装到: $PREFIX"
    mkdir -p "$BIN_DIR" "$APPS_DIR" "$ICON_DIR" "$UNIT_DIR" "$LOCAL_BIN" || die "创建目录失败"

    local f
    for f in serial-gui serial-cli serial-daemon serial-mcp; do
        if [ -f "$SRC_DIR/bin/$f" ]; then
            install -m 0755 "$SRC_DIR/bin/$f" "$BIN_DIR/$f" || die "复制 $f 失败"
        fi
    done

    # 把自己也装进去：删掉原始安装包之后，用户仍然能执行
    #   <前缀>/install.sh --fix-permissions
    #   <前缀>/install.sh --uninstall
    install -m 0755 "$SRC_DIR/install.sh" "$PREFIX/install.sh" || die "复制 install.sh 失败"

    # 图标：优先用安装包自带的
    if [ -f "$SRC_DIR/icon.png" ]; then
        install -m 0644 "$SRC_DIR/icon.png" "$ICON_FILE"
    fi

    # 桌面入口
    cat > "$DESKTOP_FILE" <<EOF
[Desktop Entry]
Type=Application
Name=串口调试工具
Name[en]=Serial Debugger
Comment=串口调试与数据分析
Exec=$BIN_DIR/serial-gui
Icon=$ICON_FILE
Terminal=false
Categories=Development;Utility;
Keywords=serial;uart;串口;调试;
StartupWMClass=serial-gui
EOF
    chmod 0644 "$DESKTOP_FILE"

    # 命令行入口（~/.local/bin 在 Ubuntu 默认 PATH 里）
    ln -sf "$BIN_DIR/serial-gui" "$LOCAL_BIN/serial-gui"

    # systemd 用户服务：随登录自动起、崩了自动拉
    cat > "$UNIT_FILE" <<EOF
[Unit]
Description=串口调试工具 守护进程
After=default.target

[Service]
Type=simple
ExecStart=$BIN_DIR/serial-daemon --silent
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF
    chmod 0644 "$UNIT_FILE"

    if command -v systemctl >/dev/null 2>&1; then
        systemctl --user daemon-reload >/dev/null 2>&1 || true
        systemctl --user enable "$APP_NAME.service" >/dev/null 2>&1 \
            && say "已启用用户服务（登录时自动启动守护进程）" \
            || warn "启用用户服务失败，可在 GUI 菜单里手动启动守护进程"
        systemctl --user restart "$APP_NAME.service" >/dev/null 2>&1 || true
    fi

    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$APPS_DIR" >/dev/null 2>&1

    say ""
    say "安装完成。"
    say "  启动方式一：应用菜单里搜索「串口调试工具」"
    say "  启动方式二：命令行执行 serial-gui"
    say "  修复串口权限：$PREFIX/install.sh --fix-permissions"
    say "  卸载：$PREFIX/install.sh --uninstall"

    # 串口权限
    if need_permission_fix; then
        say ""
        say "检测到插着串口设备，但当前用户没有读写权限。"
        say "可以用 uaccess 规则一次性解决（不需要注销，插拔即生效）。"
        if [ "$assume_yes" = "yes" ]; then
            say "（--yes 已指定，跳过自动修复；可稍后执行 --fix-permissions）"
        else
            printf "现在修复吗？[Y/n] "
            local ans=""
            read -r ans || ans=""
            case "$ans" in
                ""|y|Y|yes|YES) fix_permissions ;;
                *) say "  已跳过。之后可执行: $SRC_DIR/install.sh --fix-permissions" ;;
            esac
        fi
    fi
}

# ---------- 卸载 ----------
do_uninstall() {
    say "卸载 $APP_NAME"

    if command -v systemctl >/dev/null 2>&1; then
        systemctl --user disable --now "$APP_NAME.service" >/dev/null 2>&1 || true
    fi
    rm -f "$UNIT_FILE" "$DESKTOP_FILE" "$ICON_FILE" "$LOCAL_BIN/serial-gui"
    command -v systemctl >/dev/null 2>&1 && systemctl --user daemon-reload >/dev/null 2>&1 || true
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$APPS_DIR" >/dev/null 2>&1

    if [ -d "$PREFIX" ]; then
        rm -rf "$PREFIX"
    fi

    say "已删除程序、桌面入口与用户服务。"
    say "注意：系统级 udev 规则（$UDEV_RULE）需要管理员权限才能删，"
    say "      如要一并清理：sudo rm -f $UDEV_RULE && sudo udevadm control --reload-rules"
}

MODE="install"
ASSUME_YES="no"
for arg in "$@"; do
    case "$arg" in
        --yes|-y)          ASSUME_YES="yes" ;;
        --uninstall)       MODE="uninstall" ;;
        --fix-permissions) MODE="fixperms" ;;
        --help|-h)         print_help; exit 0 ;;
        *) die "未知参数: $arg" ;;
    esac
done

case "$MODE" in
    install)   do_install "$ASSUME_YES" ;;
    uninstall) do_uninstall ;;
    fixperms)  fix_permissions ;;
esac
