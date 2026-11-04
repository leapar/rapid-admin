// swagger-ui 初始化 —— spec 由本服务实时生成(/swagger/doc.json)
//
// ⚠️ **url 用相对路径** —— 挂载点改了(/swagger → /api-docs)不用动这个文件。
window.onload = function () {
  window.ui = SwaggerUIBundle({
    url: "./doc.json",
    dom_id: "#swagger-ui",
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    plugins: [SwaggerUIBundle.plugins.DownloadUrl],
    layout: "StandaloneLayout",
    docExpansion: "none",
    operationsSorter: "alpha",
    tagsSorter: "alpha",
    // ⚠️ persistAuthorization:false —— 别把 Bearer token 存进 localStorage。
    //   这也是上游默认值,这里显式写出来是为了"以后有人改它"时能被看见。
    persistAuthorization: false,
  });
};
