import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Bell } from 'lucide-react'
import { api, type ActivityEvent } from '@/lib/api'
import { formatRelative, cn } from '@/lib/utils'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'

const SEEN_KEY = 'tcp_activity_seen'

function loadSeen(): Set<string> {
  try {
    const raw = localStorage.getItem(SEEN_KEY)
    if (!raw) return new Set()
    return new Set(JSON.parse(raw) as string[])
  } catch {
    return new Set()
  }
}

function saveSeen(ids: Set<string>) {
  const arr = [...ids].slice(-200)
  localStorage.setItem(SEEN_KEY, JSON.stringify(arr))
}

export function ActivityBell({
  align = 'end',
  side = 'bottom',
}: {
  /** Horizontal anchor relative to the bell button. */
  align?: 'start' | 'end'
  /** Where the panel opens. */
  side?: 'bottom' | 'top' | 'right'
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [unread, setUnread] = useState(0)
  const [now, setNow] = useState(Date.now())
  const seenRef = useRef<Set<string>>(loadSeen())
  const knownRef = useRef<Set<string>>(new Set())
  const { push } = useToast()
  const navigate = useNavigate()

  const refresh = useCallback(async () => {
    try {
      const list = await api.activity(20)
      setEvents(list)
      const seen = seenRef.current
      let n = 0
      for (const e of list) {
        if (!seen.has(e.id)) n++
        if (
          e.kind === 'key_killed_auto' &&
          !knownRef.current.has(e.id) &&
          knownRef.current.size > 0
        ) {
          push({
            title: t('activity.killSwitchToast', { subject: e.subject }),
            description: e.summary,
            tone: 'danger',
            action: {
              label: t('common.view'),
              onClick: () => navigate('/activity'),
            },
          })
        }
        knownRef.current.add(e.id)
      }
      if (knownRef.current.size === 0) {
        for (const e of list) knownRef.current.add(e.id)
      }
      setUnread(n)
    } catch (err) {
      console.error(err)
    }
  }, [navigate, push, t])

  useEffect(() => {
    void refresh()
    const id = window.setInterval(() => void refresh(), 30_000)
    const tick = window.setInterval(() => setNow(Date.now()), 5 * 60_000)
    return () => {
      window.clearInterval(id)
      window.clearInterval(tick)
    }
  }, [refresh])

  const markSeen = () => {
    const next = new Set(seenRef.current)
    for (const e of events) next.add(e.id)
    seenRef.current = next
    saveSeen(next)
    setUnread(0)
  }

  return (
    <div className="relative">
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={
          unread
            ? t('activity.bellAriaUnread', { count: unread })
            : t('activity.bellAria')
        }
        onClick={() => {
          setOpen((v) => !v)
          if (!open) markSeen()
        }}
      >
        <Icon icon={Bell} />
        {unread > 0 && (
          <span className="absolute right-0.5 top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-semibold tabular-nums text-destructive-foreground">
            {unread > 9 ? '9+' : unread}
          </span>
        )}
      </Button>
      {open && (
        <>
          <button
            type="button"
            className="fixed inset-0 z-30 cursor-default"
            aria-label={t('activity.closeAria')}
            onClick={() => setOpen(false)}
          />
          <div
            className={cn(
              'absolute z-40 w-80 overflow-hidden rounded-lg border border-border bg-card shadow-md',
              side === 'bottom' && align === 'end' && 'right-0 top-full mt-2',
              side === 'bottom' && align === 'start' && 'left-0 top-full mt-2',
              side === 'top' && align === 'end' && 'right-0 bottom-full mb-2',
              side === 'top' && align === 'start' && 'left-0 bottom-full mb-2',
              side === 'right' && 'left-full top-0 ml-2',
            )}
          >
            <div className="border-b border-border px-3 py-2 text-sm font-semibold">
              {t('activity.title')}
            </div>
            <ul className="max-h-80 overflow-y-auto">
              {events.length === 0 ? (
                <li className="px-3 py-6 text-center text-xs text-muted-foreground">
                  {t('activity.noEvents')}
                </li>
              ) : (
                events.map((e) => (
                  <li
                    key={e.id}
                    className={cn(
                      'flex gap-2 border-b border-border px-3 py-2.5 last:border-0',
                      e.kind === 'key_killed_auto' && 'bg-destructive/5',
                    )}
                  >
                    <div className="min-w-0 flex-1">
                      <p className="line-clamp-2 text-sm font-medium text-foreground">{e.summary}</p>
                      <p className="mt-0.5 line-clamp-1 text-xs text-muted-foreground">{e.kind}</p>
                    </div>
                    <time
                      className="shrink-0 text-xs tabular-nums text-muted-foreground"
                      title={e.created_at}
                    >
                      {formatRelative(e.created_at, now)}
                    </time>
                  </li>
                ))
              )}
            </ul>
            <div className="border-t border-border p-2">
              <Button
                variant="ghost"
                size="sm"
                className="w-full"
                onClick={() => {
                  setOpen(false)
                  navigate('/activity')
                }}
              >
                {t('activity.viewAll')}
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
