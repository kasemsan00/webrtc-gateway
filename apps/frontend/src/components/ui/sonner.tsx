import { Toaster as Sonner } from 'sonner'
import 'sonner/dist/styles.css'
import type { ToasterProps } from 'sonner'

import { useTheme } from '@/lib/theme'

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme } = useTheme()

  return (
    <Sonner
      theme={theme}
      position="top-center"
      richColors
      closeButton
      {...props}
    />
  )
}

export { Toaster }
