package asrx

import "encoding/json"

// jsonUnmarshal 测试文件内的小别名 —— 让 fake 服务端不用各处 import encoding/json
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
