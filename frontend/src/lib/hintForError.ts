/**
 * Map create/index errors to actionable fix-it hints for the Add-server dialog.
 * Lives in the frontend only — no backend change.
 * Hints are UI chrome (t()-keyed); API error strings stay verbatim at call sites.
 */

import { i18n } from '@/i18n'

export type HintContext = {
  keyLabel?: string
  command?: string
  host?: string
}

export type HintTranslate = (key: string, opts?: Record<string, unknown>) => string

export function hintForError(
  msg: string,
  ctx: HintContext = {},
  t: HintTranslate = (key, opts) => i18n.t(key, opts),
): string {
  const m = (msg || '').toLowerCase()
  if (!m) return ''

  if (
    /\b401\b/.test(m) ||
    m.includes('unauthorized') ||
    m.includes('missing token') ||
    m.includes('invalid token') ||
    m.includes('authentication') ||
    m.includes('auth required')
  ) {
    const label = ctx.keyLabel || t('hints.defaultKeyLabel')
    return t('hints.auth', { label })
  }

  if (/\b404\b/.test(m) || (m.includes('not found') && !m.includes('command'))) {
    return t('hints.notFound')
  }

  if (
    m.includes('command not found') ||
    m.includes('not found in path') ||
    m.includes('executable file not found') ||
    m.includes('no such file')
  ) {
    const cmd = ctx.command || t('hints.defaultCommand')
    return t('hints.command', { cmd })
  }

  if (
    m.includes('timeout') ||
    m.includes('deadline exceeded') ||
    m.includes('connection refused') ||
    m.includes('no such host') ||
    m.includes('network') ||
    m.includes('i/o timeout') ||
    m.includes('dial tcp')
  ) {
    const host = ctx.host || t('hints.defaultHost')
    return t('hints.network', { host })
  }

  return ''
}

/** Host portion of a URL for timeout hints; empty if unparseable. */
export function hostFromUrl(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url || 'host'
  }
}
