// SMS sender factory —— 按 config.SMS.Provider 选实现
//
// 注意: 此文件与 aliyun / console 在不同包, 避免 msn → aliyun → msn 的 import cycle.
// 实际调用方需要自行 import 各子包并按 provider 选实现. 详见 cmd/server/main.go 的组装代码.
package msn
