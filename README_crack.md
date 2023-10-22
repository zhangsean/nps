# 新增API控制nps端
  nps.conf 新增API接口通信签名key
   ```
   api_sign_code=123456
   ```
 
# 签名验证逻辑
   ```
   // 验证请求的签名是否正确
func verifySignature(ctx *context.Context, signKey string) bool {
	signatureBase64 := ctx.Request.Header.Get("X-Signature")
	if signatureBase64 == "" {
		return false
	}
	postForm := ctx.Request.PostForm
	requestURI := ctx.Request.RequestURI
	// 将RequestURI 和 PostForm 按照首字母升序排序，并用###间隔拼接成字符串
	var keys []string
	for k := range postForm {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var params []string
	for _, k := range keys {
		v := strings.Join(postForm[k], ",")
		params = append(params, fmt.Sprintf("%s=%s", k, v))
	}
	data := []byte(requestURI + "###" + strings.Join(params, "&"))
	bodyHash := sha256.Sum256(data)

	mac := hmac.New(sha256.New, []byte(signKey))
	mac.Write(bodyHash[:])
	signature := mac.Sum(nil)

	// 将签名进行 Base64 编码，方便比较
	expectedSignatureBase64 := base64.StdEncoding.EncodeToString(signature)
	if beego.AppConfig.String("runmode") == "dev" {
		beego.BeeLogger.Debug("api request data: %s", data)
		beego.BeeLogger.Debug("api request signatureBase64: %s expectedSignatureBase64: %s", signatureBase64, expectedSignatureBase64)
	}
	return signatureBase64 == expectedSignatureBase64
}
   ```