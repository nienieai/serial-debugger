#!/bin/bash
# 串口调试工具 —— Linux 用户态安装包打包脚本
#
# 产出两个东西，内容相同：
#   build/pkg/serial-tool-<ver>-linux-amd64.tar.gz   解压后执行 install.sh
#   build/pkg/serial-tool-<ver>-linux-amd64.run      单文件自解压，双击/直接执行
#
# 用法：
#   ./build/linux/make-package.sh [版本号]
#   版本号缺省读 wails.json 的 productVersion
#
# 为什么不做 deb：这个软件的 settings/、logs/、history/、multistr/ 都写在 exe
# 旁边，装到 /usr/bin 这类只读目录会全部失效。用户态安装才留得住这些状态。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    VERSION="$(sed -n 's/.*"productVersion": *"\([^"]*\)".*/\1/p' wails.json | head -n1)"
fi
[ -n "$VERSION" ] || { echo "无法确定版本号，请显式传入" >&2; exit 1; }

PKG_NAME="serial-tool-$VERSION-linux-amd64"
STAGE="build/pkg/$PKG_NAME"
OUT_DIR="build/pkg"
LDFLAGS="-w -s -X github.com/nienieai/serial-debugger/version.Version=$VERSION"

echo "==> 版本: $VERSION"
echo "==> 构建二进制"
mkdir -p build/bin
go build -tags production -trimpath -p 2 \
    -ldflags "$LDFLAGS" -o build/bin/serial-gui .
go build -trimpath -p 2 -ldflags "$LDFLAGS" -o build/bin/serial-cli    ./cmd/serial-cli
go build -trimpath -p 2 -ldflags "$LDFLAGS" -o build/bin/serial-daemon ./daemon/
go build -trimpath -p 2 -ldflags "$LDFLAGS" -o build/bin/serial-mcp    ./cmd/serial-mcp

echo "==> 组装 $STAGE"
rm -rf "$STAGE"
mkdir -p "$STAGE/bin" "$OUT_DIR"
cp build/bin/serial-gui    "$STAGE/bin/"
cp build/bin/serial-cli    "$STAGE/bin/"
cp build/bin/serial-daemon "$STAGE/bin/"
cp build/bin/serial-mcp    "$STAGE/bin/"
cp build/linux/install.sh  "$STAGE/"
chmod +x "$STAGE/install.sh"
cp assets/icons/gui_256.png "$STAGE/icon.png"
cp assets/icons/gui.svg     "$STAGE/icon.svg"

cat > "$STAGE/安装说明.txt" <<'TXT'
串口调试工具 —— Linux 安装说明（用户态，不写系统目录）

一、安装
  包内解压后执行：
      ./install.sh
  它会问你是否顺带修复串口权限（会弹一次图形密码框）。
  只想先装上、不动权限的话：
      ./install.sh --yes

二、启动
  应用菜单里搜索「串口调试工具」，或命令行执行 serial-gui。

三、串口权限
  首次插上 USB 串口设备后，如果打开失败并提示 Permission denied：
      ~/.local/lib/serial-tool/install.sh --fix-permissions
  （安装目录里已经留了一份 install.sh，原始安装包删掉也能用）
  这是把一条 uaccess 规则写进 /etc/udev/rules.d/，需要一次管理员授权。
  加完不需要注销，重新插拔设备即可。

四、卸载
      ~/.local/lib/serial-tool/install.sh --uninstall
  系统级 udev 规则需要手动删：
      sudo rm -f /etc/udev/rules.d/60-serial-tool.rules && sudo udevadm control --reload-rules

五、数据位置
  所有设置、日志、历史、多字符串预设都保存在安装目录内：
      ~/.local/lib/serial-tool/
  升级时覆盖安装目录即可，不会丢数据。
TXT

echo "==> 打 tar.gz"
TARBALL="$OUT_DIR/$PKG_NAME.tar.gz"
rm -f "$TARBALL"
tar -czf "$TARBALL" -C "$OUT_DIR" "$PKG_NAME"

echo "==> 打自解压 .run"
RUNFILE="$OUT_DIR/$PKG_NAME.run"
HEADER="$(mktemp)"
cat > "$HEADER" <<'RUNHEADER'
#!/bin/bash
# 串口调试工具 自解压安装包
# 直接执行（或双击）后会解压到临时目录并调用其中的 install.sh，参数原样透传。
set -u
LINE=$(awk '/^__ARCHIVE_BELOW__$/{print NR+1; exit}' "$0")
TMP=$(mktemp -d "${TMPDIR:-/tmp}/serial-tool-install.XXXXXX") || exit 1
trap 'rm -rf "$TMP"' EXIT
tail -n +"$LINE" "$0" | tar xzf - -C "$TMP" || { echo "解压失败" >&2; exit 1; }
# mindepth 1：临时目录自身也叫 serial-tool-install.*，不排除会匹配到自己
PKG=$(find "$TMP" -mindepth 1 -maxdepth 1 -type d -name 'serial-tool-*' | head -n1)
[ -n "$PKG" ] || { echo "安装包结构异常" >&2; exit 1; }
bash "$PKG/install.sh" "$@"
status=$?
# 必须在这里退出：marker 之后是二进制，继续往下 bash 会把它当脚本解析
exit $status
__ARCHIVE_BELOW__
RUNHEADER
cat "$HEADER" "$TARBALL" > "$RUNFILE"
chmod +x "$RUNFILE"
rm -f "$HEADER"

echo
echo "完成："
ls -lh "$TARBALL" "$RUNFILE" | sed 's/^/  /'
echo
echo "sha256:"
sha256sum "$TARBALL" "$RUNFILE" | sed 's/^/  /'
