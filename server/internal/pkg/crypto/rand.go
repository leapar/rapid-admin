// crypto 包内共用的 rand reader(避免循环依赖 / 集中 mock)
package crypto

import (
	"crypto/rand"
	"io"
)

// randReader 默认从系统 CSPRNG 读;测试可覆盖
var randReader io.Reader = rand.Reader

// readFull 包装 ReadFull
func readFull(r io.Reader, buf []byte) (int, error) {
	return io.ReadFull(r, buf)
}
