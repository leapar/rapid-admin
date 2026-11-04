/**
 * IdentitySwitcher —— 身份切换器
 *
 * 顶部下拉:展示当前用户所有身份,点击切换(绑了手机号的账号切高敏角色需短信验证;
 * 没绑手机号的免验直接切 —— 2026-11-03 用户拍板,后端 SwitchIdentity 同口径)
 *
 * Phase 5 M8 修死代码:
 *   - 之前 handleSwitch(identity, needSms=true) 时 highRisk 直接 return,不调任何东西
 *   - 现在高敏角色弹 SmsCodeGuard,验证通过后再调 switchIdentity
 *   - 切换成功后通过 fetchAccessible 刷新权限(不 reload)
 */
import React, { useState } from 'react'
import { Dropdown, Space, Avatar, Typography, Tag, Modal, App, Button } from 'antd'
import { DownOutlined, SafetyOutlined } from '@ant-design/icons'
import { useAuth } from '@/hooks/useAuth'
import { useI18n } from '@/locales'
import { useUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
import { switchIdentity, sendAccountSms } from '@/api/auth'
import { SmsCodeGuard } from '@/components/common'
import { avatarInitial } from '@/components/common/avatar'
import { Identity, UserInfo } from '@/types/user'
import { RoleCode, ROLE_NAME_MAP } from '@/constants/roles'
import { SWITCH_IDENTITY_SMS_SCENE } from '@/constants/sms'

const { Text } = Typography

/** 高敏感角色(切换需短信验证) */
const HIGH_RISK_ROLES: string[] = [
  RoleCode.SUPER_ADMIN,
  RoleCode.PLATFORM_OPS,
]

const isHighRisk = (roleCode?: string) =>
  !!roleCode && HIGH_RISK_ROLES.includes(roleCode)

export const IdentitySwitcher: React.FC = () => {
  const { message } = App.useApp()
  const { t } = useI18n()
  const { userInfo, currentIdentity } = useAuth()
  const switchIdentityStore = useUserStore((s) => s.switchIdentity)
  const fetchAccessible = usePermissionStore((s) => s.fetchAccessible)
  const [loading, setLoading] = useState(false)
  const [smsTarget, setSmsTarget] = useState<Identity | null>(null)

  /**
   * 角色显示名
   *
   * ⚠️ **M103:`ROLE_NAME_MAP[code]` 返回的是 i18n key(`role.superAdmin`),
   *   不是已翻译文案** —— 直接把它渲染出来,用户看到的是 "role.superAdmin"
   *   这种原始 key。必须过 `t()`。
   *
   *   以前查表几乎次次落空(前端那套 role code 是错的)→ 一直回落到后端
   *   返回的中文 `role_name`,**反而"看着正常"**;M103 把角色 code 修对之后,
   *   这个洞才露出来 —— 所以它跟 B2 是同一个修复的两面。
   *
   *   查不到(后台自建角色)时回落后端的中文名 —— 那总比显示 key 好。
   */
  const roleLabel = (identity: Identity): string => {
    const key = ROLE_NAME_MAP[identity.role_code]
    if (key) return t(key)
    return identity.role_name || identity.role_code
  }

  if (!userInfo) return null

  const identities = userInfo.identities || []

  /** 实际切换身份(可能已通过 SMS 验证) */
  const doSwitch = async (target: Identity, smsCode?: string) => {
    setLoading(true)
    try {
      // ⚠️ 这里原来包了一层 String():那时 id 是 number,而 wire 上是 19 位字符串。
      // ID 改字符串之后这层转换纯冗余 —— 万一哪天的 id 又变回 number,字符串化
      // 会把问题重新藏起来,不如让类型系统直接拦住。
      const resp = await switchIdentity(target.id, smsCode)
      // 2026-10-21:后端返 AuthTokens —— current_identity_id 是服务端权威值,
      //   从 user.identities 里按它取回完整身份对象。此前这里读的是 resp.identity,
      //   而后端从来没有这个字段,所以恒为 undefined,一直靠 target 兜底。
      const newId = resp.user?.current_identity_id || target.id
      const newIdentity = resp.user?.identities?.find((i) => i.id === newId) ?? target
      // 服务端同时签发了新的 access / refresh token,两个都要落盘。
      // 之前只写 access —— 新 refresh 被丢掉,浏览器继续用旧的那个。
      // 配合 W7 的单次使用轮换,这会让"切身份"成为一次隐性续期,
      // 旧 refresh 被消费掉而新值没人存 → 下次自动续期直接失败。
      //
      // ⚠️ M146b:把 resp.user 一起交出去 —— 它是服务端对"你是谁 / 当前是谁"
      //   的权威回答。不交的话 userInfo.current_identity_id 停在旧身份上,
      //   个人中心显示的还是切之前的角色名。
      //   契约上 `user` 是必填的(LoginResponse.user: UserInfo),但万一后端
      //   漏返,也不能把 userInfo 打成空对象 —— 那样头像/角色名/身份列表
      //   会一起变空。兜底:当前 userInfo + 新的 current_identity_id。
      const serverUser: UserInfo = resp.user ?? {
        ...userInfo,
        current_identity_id: newId,
      }
      switchIdentityStore(
        newIdentity,
        {
          accessToken: resp.token,
          expiresIn: resp.expires_in,
          refreshToken: resp.refresh_token,
          refreshExpiresIn: resp.refresh_expires_in,
        },
        serverUser,
      )
      // 刷新当前用户可访问资源(不 reload 页面)
      let permRefreshFailed = false
      try {
        await fetchAccessible()
      } catch {
        // ⚠️ M146b:以前这里是 `window.location.reload()`。
        //   那等于"切完身份后把整个应用重启一遍",而它带来的后果正好是
        //   「切完就退出」这条链的最后一环:
        //     切换本身成功(新 token 已落盘)→ 但刷新权限失败(403)→ reload
        //     → PermissionLoader 重新拉 /auth/me → 同样 403
        //     → 旧代码把 403 当"登录态失效" → 清 token → 跳登录页。
        //   而且 reload 还会丢掉 SPA 的全部内存态(填了一半的表单、
        //   展开的表格、滚动位置)。
        //   现在:保留登录态 + 明确告诉用户"切换已生效,但权限没刷出来"。
        //   用户可以在右上角再切一次。
        permRefreshFailed = true
      }
      if (permRefreshFailed) {
        message.warning(t('component.identitySwitcher.permissionRefreshFailed'))
        return
      }
      message.success(
        t('component.identitySwitcher.switchedTo', undefined, {
          name: roleLabel(newIdentity),
        }),
      )
    } catch {
      // 错误已由 axios 拦截器处理,这里不再 toast
    } finally {
      setLoading(false)
    }
  }

  const handleClickIdentity = (target: Identity) => {
    if (currentIdentity?.id === target.id) return
    if (isHighRisk(target.role_code) && userInfo.phone) {
      // 高敏角色且账号登记了手机号:弹 SMS 验证 Modal
      //
      // ⚠️ userInfo.phone 是**脱敏值**(138****1234),有值 ⇔ 后端
      //   sys_user.phone 非空 —— 正好当"绑没绑手机号"的判据用。
      //   2026-11-03 用户拍板:没绑手机号 → 免验直接切(后端同口径:
      //   SwitchIdentity 对 callerPhone == "" 跳过短信校验)。
      setSmsTarget(target)
    } else {
      // 普通身份,或没绑手机号的高敏身份:直接切换
      doSwitch(target)
    }
  }

  const items = identities.map((id) => ({
    key: id.id,
    label: (
      <Space>
        <Tag color={id.id === currentIdentity?.id ? 'blue' : 'default'}>
          {roleLabel(id)}
        </Tag>
        <Text type={id.id === currentIdentity?.id ? 'success' : undefined}>
          {id.project_name ? `${id.project_name} / ` : ''}
          {id.role_name || ''}
        </Text>
        {isHighRisk(id.role_code) && (
          <Tag color="red" icon={<SafetyOutlined />}>
            {t('component.identitySwitcher.highSensitive')}
          </Tag>
        )}
      </Space>
    ),
    disabled: id.id === currentIdentity?.id,
  }))

  const trigger = (
    <a onClick={(e) => e.preventDefault()} style={{ color: 'inherit' }}>
      <Space>
        <Avatar size="small" style={{ backgroundColor: '#1677ff' }}>
          {/* ⚠️ M113:`nickname?.[0]` 是 **UTF-16 码元**下标,不是码点下标 ——
              emoji / 𠮷 这类**代理对**字符会被切成半个代理对(渲染成豆腐块);
              中文 / 拉丁文在 BMP 里,两种切法结果一样。
              改用按**码点**切的 avatarInitial(与 UserAvatar 同一个)。 */}
          {avatarInitial(userInfo.nickname) || 'U'}
        </Avatar>
        <Text style={{ color: 'var(--app-text)' }}>
          {userInfo.nickname || userInfo.username}
          {currentIdentity && (
            <Tag color="blue" style={{ marginLeft: 8 }}>
              {roleLabel(currentIdentity)}
            </Tag>
          )}
        </Text>
        <DownOutlined style={{ fontSize: 10, color: 'var(--app-text-tertiary)' }} />
      </Space>
    </a>
  )

  return (
    <>
      <Dropdown
        menu={{
          items,
          onClick: ({ key }) => {
            const target = identities.find((i) => i.id === key)
            if (target) handleClickIdentity(target)
          },
        }}
        placement="bottomRight"
        trigger={['click']}
        disabled={loading}
      >
        {trigger}
      </Dropdown>

      {/* 高敏感身份切换 — 短信二次验证 Modal */}
      <Modal
        title={t('component.identitySwitcher.switchTitle')}
        open={!!smsTarget}
        footer={null}
        onCancel={() => setSmsTarget(null)}
        destroyOnHidden
        maskClosable={false}
      >
        <div style={{ padding: '8px 0' }}>
          <p style={{ color: 'var(--app-text-secondary)' }}>
            {t('component.identitySwitcher.switchToPrefix')}{' '}
            <b style={{ color: 'var(--app-primary)' }}>{smsTarget?.role_name}</b>{' '}
            {t('component.identitySwitcher.switchToSuffix')}
          </p>
          <SmsCodeGuard
            scene={SWITCH_IDENTITY_SMS_SCENE}
            phone={userInfo.phone}
            title={t('smsGuard.verifyTitle')}
            // ⚠️ **mode="delegate"**:这个码由 switchIdentity 自己消费
            //   (后端校验通过即置 used_at)。本地再验一次的话,
            //   后面那次必然拿到「短信验证码已使用」。
            mode="delegate"
            // 短信由**服务端**代发 —— 前端只有脱敏号(138****5678),
            // 拿它发短信后端会拒(validPhone 要求 11 位纯数字)。
            sendCode={async () => {
              if (!smsTarget) return
              await sendAccountSms(SWITCH_IDENTITY_SMS_SCENE)
            }}
            trigger={
              <Button type="primary" block>
                {t('sms.send')}
              </Button>
            }
            onVerified={async (code) => {
              // 把验证码**原样**交给 switchIdentity —— 后端会拿它跟
              // "账号登记的手机号 + scene=change_phone"比对(见 M104 的绑定修复)。
              const target = smsTarget
              setSmsTarget(null)
              if (target) await doSwitch(target, code)
            }}
          />
        </div>
      </Modal>
    </>
  )
}

export default IdentitySwitcher
