import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface CopyButtonProps {
  text: string
  label?: string
  className?: string
  size?: 'sm' | 'default' | 'icon'
  variant?: 'outline' | 'secondary' | 'ghost' | 'default'
}

export function CopyButton({
  text,
  label = 'Copy',
  className,
  size = 'sm',
  variant = 'outline',
}: CopyButtonProps) {
  const [copied, setCopied] = useState(false)

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch (err) {
      console.error('copy failed', err)
    }
  }

  return (
    <Button
      type="button"
      variant={variant}
      size={size}
      className={cn(className)}
      onClick={() => void onCopy()}
      aria-label={copied ? 'Copied' : label}
    >
      {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
      {copied ? 'Copied ✓' : label}
    </Button>
  )
}
