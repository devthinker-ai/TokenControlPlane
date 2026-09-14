import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'

export type ToastTone = 'default' | 'warning' | 'danger' | 'success'

export interface ToastAction {
  label: string
  onClick: () => void
}

export interface ToastItem {
  id: string
  title: string
  description?: string
  tone?: ToastTone
  action?: ToastAction
}

interface ToastContextValue {
  push: (t: Omit<ToastItem, 'id'>) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast requires ToastProvider')
  return ctx
}

const toneBar: Record<ToastTone, string> = {
  default: 'bg-primary',
  success: 'bg-emerald-500',
  warning: 'bg-amber-500',
  danger: 'bg-destructive',
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const [items, setItems] = useState<ToastItem[]>([])

  const push = useCallback((toast: Omit<ToastItem, 'id'>) => {
    const id = `toast_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`
    setItems((prev) => [...prev, { ...toast, id }].slice(-3))
    window.setTimeout(() => {
      setItems((prev) => prev.filter((x) => x.id !== id))
    }, 5000)
  }, [])

  const value = useMemo(() => ({ push }), [push])

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-[min(100vw-2rem,22rem)] flex-col gap-2"
        aria-live="polite"
      >
        {items.map((item) => (
          <div
            key={item.id}
            className="toast-enter pointer-events-auto overflow-hidden rounded-lg border border-border bg-card text-sm shadow-md"
            role="status"
          >
            <div className="flex">
              <div
                className={cn('w-0.5 shrink-0', toneBar[item.tone ?? 'default'])}
                aria-hidden
              />
              <div className="flex min-w-0 flex-1 items-start gap-2 p-3">
                <div className="min-w-0 flex-1">
                  <p className="font-medium text-foreground">{item.title}</p>
                  {item.description && (
                    <p className="mt-0.5 text-xs text-muted-foreground">{item.description}</p>
                  )}
                  {item.action && (
                    <Button
                      size="sm"
                      variant="secondary"
                      className="mt-2"
                      onClick={item.action.onClick}
                    >
                      {item.action.label}
                    </Button>
                  )}
                </div>
                <button
                  type="button"
                  className="rounded p-1 text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  aria-label={t('common.dismiss')}
                  onClick={() => setItems((prev) => prev.filter((x) => x.id !== item.id))}
                >
                  <Icon icon={X} className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}
