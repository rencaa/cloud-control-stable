// 默认配置
const defaultConfig = {
  site: {
    name: '通用云控系统',
    title: '通用云控系统',
    description: '专业的设备管理与控制系统',
    keywords: '云控,设备管理,自动化,远程控制',
    copyright: '© 2026 通用云控系统. All rights reserved.',
    favicon: '/favicon.ico',
    logo: '',
  },
  login: {
    title: '通用云控系统',
    background: '#000000',
    backgroundImage: '/bg.jpg',
    logo: '',
    showLogo: false,
    showBackgroundImage: true,
  },
  theme: {
    primaryColor: '#1890ff',
    successColor: '#52c41a',
    warningColor: '#faad14',
    dangerColor: '#f5222d',
  },
  features: {
    enableWebSocket: true,
    enableNotification: true,
  },
}

let mergedConfig = { ...defaultConfig }
let configLoaded = false

export async function setupConfig() {
  try {
    const res = await fetch('/config.json?t=' + Date.now())
    if (res.ok) {
      const external = await res.json()
      mergedConfig = deepMerge(defaultConfig, external)
    }
  } catch {
    console.warn('加载外部配置失败，使用默认配置')
  }
  configLoaded = true
  updateFavicon()
  updateTitle()
}

export function getConfig() {
  return mergedConfig
}

export function getSiteName() {
  return mergedConfig.site.name
}

export function getLoginBackground() {
  const login = mergedConfig.login
  if (login.showBackgroundImage && login.backgroundImage) {
    return `url(${login.backgroundImage}) center center / cover no-repeat fixed`
  }
  return login.background
}

function deepMerge(target, source) {
  const result = { ...target }
  for (const key in source) {
    if (typeof source[key] === 'object' && !Array.isArray(source[key])) {
      result[key] = deepMerge(target[key] || {}, source[key])
    } else {
      result[key] = source[key]
    }
  }
  return result
}

function updateFavicon() {
  const favicon = mergedConfig.site.favicon || '/favicon.ico'
  let link = document.querySelector("link[rel~='icon']")
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }
  link.href = favicon
}

function updateTitle() {
  document.title = mergedConfig.site.title || mergedConfig.site.name
}
