import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Check, Copy, AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import type { ClientSnippets } from '@/lib/api'

interface KeyRevealProps {
  plaintextKey: string
  snippets?: ClientSnippets
}

async function copyText(text: string) {
  await navigator.clipboard.writeText(text)
}

export function KeyReveal({ plaintextKey, snippets }: KeyRevealProps) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState<string | null>(null)

  const copy = async (label: string, text: string) => {
    await copyText(text)
    setCopied(label)
    setTimeout(() => setCopied(null), 1500)
  }

  return (
    <div className="space-y-4">
      <div
        role="alert"
        className="flex gap-3 rounded-md border border-amber-500/40 bg-amber-500/10 p-4 text-amber-200"
      >
        <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0" />
        <div>
          <p className="font-semibold">{t('keys.revealOnce')}</p>
          <p className="mt-1 text-sm text-amber-200/80">{t('keys.revealHint')}</p>
        </div>
      </div>

      <div className="rounded-md border border-border bg-background p-3">
        <code className="block break-all text-sm text-foreground">{plaintextKey}</code>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-3"
          onClick={() => copy('key', plaintextKey)}
        >
          {copied === 'key' ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
          {copied === 'key' ? t('common.copied') : t('keys.copyKey')}
        </Button>
      </div>

      {snippets && (
        <div className="space-y-2">
          <p className="text-sm font-medium text-muted-foreground">{t('keys.clientSnippets')}</p>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => copy('claude', snippets.claude_desktop)}
            >
              {copied === 'claude' ? t('common.copied') : 'Claude Desktop'}
            </Button>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => copy('cursor', snippets.cursor)}
            >
              {copied === 'cursor' ? t('common.copied') : 'Cursor'}
            </Button>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => copy('windsurf', snippets.windsurf)}
            >
              {copied === 'windsurf' ? t('common.copied') : 'Windsurf'}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
