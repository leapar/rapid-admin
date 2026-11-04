// swaggerx JSON 序列化辅助
// 单独放一个文件,避免 main 端 import 时把整个 swaggerx 拉进编译
package swaggerx

import "encoding/json"

func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
