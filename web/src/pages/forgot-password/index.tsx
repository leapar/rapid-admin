/**
 * 找回密码  — 独立页面 (公开, 无需登录)
 *
 * 单屏表单, 一次性提交, 没有 Modal 多步骤:
 *   1. 手机号                 → 实时校验
 *   2. 短信验证码             → 配 SmsSendButton, 60s 倒计时
 *   3. 新密码 + 确认新密码    → 本地一致性 + 服务端强度规则 (8-32 位 + 字母 + 数字)
 *   4. 单个 "提交重置" 按钮
 *
 * 提交成功后整页切换到 "已完成" 视图, 无 Modal.
 */
import React, { useState } from 'react'
import {
  Card, Form, Input, Button, Typography, Space, Result, Row, Col, message,
} from 'antd'
import {
  MobileOutlined, SafetyOutlined, LockOutlined, KeyOutlined,
} from '@ant-design/icons'
import { Link, useNavigate } from 'react-router-dom'
import { resetPasswordBySms } from '@/api/auth'
import { usePublicConfigStore } from '@/stores/publicConfig'
import { SmsSendButton, CaptchaField } from '@/components/common'
import { ROUTES } from '@/constants/app'
import { SmsScene } from '@/constants/sms'
import { useI18n } from '@/locales'

const { Title, Text } = Typography

const PHONE_RE = /^1[3-9]\d{9}$/
const SMS_CODE_LEN = 6
/** 必须同时含字母和数字, 8-32 位 */
const STRONG_PWD_RE = /^(?=.*[A-Za-z])(?=.*\d).{8,32}$/

interface ForgotFormValues {
  phone: string
  code: string
  /** 图形验证码 { id, code }(单次使用, 失败/成功都强制换图)
   *  可选:相关开关关闭时字段不渲染,提交时为 undefined */
  captcha?: { id: string; code: string }
  password: string
  confirmPassword: string
}

const ForgotPassword: React.FC = () => {
  const { t } = useI18n()
  const [form] = Form.useForm<ForgotFormValues>()
  const phone = Form.useWatch('phone', form) || ''
  // 本页只有短信一条路径,验证码框取决于 login_enabled || sms_send_enabled ——
  // 流程第一步是调 /public/auth/sms/send,只要发短信闸门还开着就得留着。
  // 未拉到时按 true(见 stores/publicConfig.ts)。
  const captchaEnabled = usePublicConfigStore((s) => s.isSmsCaptchaEnabled())
  const [submitting, setSubmitting] = useState(false)
  const [done, setDone] = useState(false)
  // 触发 CaptchaField 重新拉图: 短信发送失败 / 提交失败 时调用
  const [captchaKey, setCaptchaKey] = useState(0)
  const navigate = useNavigate()

  const onFinish = async (values: ForgotFormValues) => {
    setSubmitting(true)
    try {
      await resetPasswordBySms(values.phone, values.code, values.password, values.captcha)
      message.success(t('message.resetPasswordSuccess'))
      setDone(true)
    } catch {
      // 提交失败时: 短信码/captcha 都已被服务端单次消耗, 强制换 captcha 图
      setCaptchaKey(Date.now())
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div style={styles.page}>
      <Row justify="center" align="middle" style={{ width: '100%' }}>
        <Col xs={22} sm={20} md={16} lg={12} xl={8}>
          <Card style={styles.card} hoverable>
            {done ? (
              <Result
                status="success"
                icon={<KeyOutlined style={{ color: '#52c41a' }} />}
                title={t('forgotPassword.successTitle')}
                subTitle={t('forgotPassword.successSubtitle')}
                extra={
                  <Button type="primary" size="large" onClick={() => navigate(ROUTES.LOGIN)}>
                    {t('forgotPassword.goLogin')}
                  </Button>
                }
              />
            ) : (
              <>
                <Space direction="vertical" size={4} style={{ width: '100%', marginBottom: 24 }}>
                  <Title level={3} style={{ margin: 0, textAlign: 'center' }}>
                    {t('forgotPassword.title')}
                  </Title>
                  <Text type="secondary" style={{ display: 'block', textAlign: 'center' }}>
                    {t('forgotPassword.subtitle')}
                  </Text>
                </Space>

                <Form<ForgotFormValues>
                  form={form}
                  layout="vertical"
                  onFinish={onFinish}
                  requiredMark={false}
                  size="large"
                  autoComplete="off"
                >
                  <Form.Item
                    name="phone"
                    label={t('common.phone')}
                    rules={[
                      { required: true, message: t('validation.phoneRequired') },
                      { pattern: PHONE_RE, message: t('validation.phoneInvalid') },
                    ]}
                  >
                    <Input
                      prefix={<MobileOutlined />}
                      placeholder={t('forgotPassword.phonePlaceholder')}
                      maxLength={11}
                    />
                  </Form.Item>

                  {/* 验证码字段: 外层 Form.Item 只做 label 容器, 内层 noStyle 做绑定 */}
                  <Form.Item label={t('login.smsCode')} required>
                    <div style={{ display: 'flex', gap: 8, width: '100%' }}>
                      <Form.Item
                        name="code"
                        noStyle
                        rules={[
                          { required: true, message: t('validation.smsCodeRequired') },
                          { len: SMS_CODE_LEN, message: t('validation.smsCodeLen', undefined, { n: SMS_CODE_LEN }) },
                        ]}
                      >
                        <Input
                          prefix={<SafetyOutlined />}
                          placeholder={t('validation.smsCodePlaceholder', undefined, { n: SMS_CODE_LEN })}
                          maxLength={SMS_CODE_LEN}
                          style={{ flex: 1 }}
                        />
                      </Form.Item>
                      <div style={{ width: 140 }}>
                        <SmsSendButton
                          phone={phone}
                          scene={SmsScene.RESET_PASSWORD}
                          // 2026-10-28 M106:发短信会消费掉后端那张图形验证码
                          //   (先删后比,成功失败都消费)。提交重置那一步
                          //   (POST /public/auth/reset-password-sms,scene=login)
                          //   要再校验一次 —— 不换图就是拿一张已作废的验证码去校验,必失败。
                          onCaptchaConsumed={() => setCaptchaKey(Date.now())}
                          beforeSend={async () => {
                            try {
                              // 验证码关闭时字段未注册,只校验 phone ——
                              // 否则"手机号没填就点发送"的拦截会跟着一起消失。
                              await form.validateFields(captchaEnabled ? ['phone', 'captcha'] : ['phone'])
                              const c = captchaEnabled
                                ? (form.getFieldValue('captcha') as { id: string; code: string })
                                : undefined
                              return { ok: true, captcha: c }
                            } catch (e: any) {
                              return { ok: false, error: e?.errorFields?.[0]?.errors?.[0] ?? t('validation.captchaRequired') }
                            }
                          }}
                        />
                      </div>
                    </div>
                  </Form.Item>

                  {captchaEnabled && (
                    <Form.Item
                      name="captcha"
                      label={t('login.captcha')}
                      rules={[
                        {
                          validator: (_, v: { id?: string; code?: string } | undefined) =>
                            v?.code ? Promise.resolve() : Promise.reject(new Error(t('validation.captchaRequired'))),
                        },
                      ]}
                    >
                      <CaptchaField refreshKey={captchaKey} />
                    </Form.Item>
                  )}

                  <Form.Item
                    name="password"
                    label={t('field.newPassword')}
                    rules={[
                      { required: true, message: t('validation.newPasswordRequired') },
                      { pattern: STRONG_PWD_RE, message: t('validation.passwordStrength') },
                    ]}
                  >
                    <Input.Password
                      prefix={<LockOutlined />}
                      placeholder={t('validation.newPasswordPlaceholder')}
                      autoComplete="new-password"
                    />
                  </Form.Item>

                  <Form.Item
                    name="confirmPassword"
                    label={t('field.confirmPassword')}
                    dependencies={['password']}
                    rules={[
                      { required: true, message: t('validation.confirmPasswordRequired') },
                      ({ getFieldValue }) => ({
                        validator(_, value) {
                          if (!value || getFieldValue('password') === value) {
                            return Promise.resolve()
                          }
                          return Promise.reject(new Error(t('validation.passwordMismatch')))
                        },
                      }),
                    ]}
                  >
                    <Input.Password
                      prefix={<LockOutlined />}
                      placeholder={t('validation.confirmPasswordPlaceholder')}
                      autoComplete="new-password"
                    />
                  </Form.Item>

                  <Form.Item style={{ marginTop: 32 }}>
                    <Button
                      type="primary"
                      htmlType="submit"
                      block
                      size="large"
                      loading={submitting}
                    >
                      {t('forgotPassword.submit')}
                    </Button>
                  </Form.Item>
                </Form>

                <div style={{ textAlign: 'center' }}>
                  <Text type="secondary">
                    {t('forgotPassword.rememberBack')}{' '}
                    <Link to={ROUTES.LOGIN}>{t('forgotPassword.loginNow')}</Link>
                  </Text>
                </div>
              </>
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    background: 'linear-gradient(135deg, var(--app-primary) 0%, var(--app-primary-hover) 100%)',
    display: 'flex',
    alignItems: 'center',
    padding: '24px 16px',
  },
  card: {
    boxShadow: '0 8px 32px rgba(0,0,0,0.12)',
    borderRadius: 8,
  },
}

export default ForgotPassword
