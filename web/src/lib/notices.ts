import { filesURL, selectURL, type Notice } from './api'
import { size, parent } from './format'
import { t } from './i18n'

export function noticeText(n: Notice) {
  const f = t.noticeText[n.kind]
  if (!f) return n.kind
  if (n.kind === 'disk_low') return f(n.vol, size(n.count))
  if (n.kind === 'mention' || n.kind === 'comment') return f(n.name, n.actor)
  return f(n.name, n.share)
}

// Where a notice leads when someone opens it.
export function noticeLink(n: Notice) {
  switch (n.kind) {
    case 'share_upload':
      return n.vol && n.path ? selectURL(n.vol, n.path) : '/shares'
    case 'mention':
    case 'comment':
      return `${filesURL(n.vol, parent(n.path))}?${new URLSearchParams({ details: n.name, tab: 'comments' })}`
    case 'share_download':
      return '/shares'
    case 'joined':
      return '/team'
    case 'disk_low':
      return '/storage'
  }
  return '/'
}
