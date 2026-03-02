import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRoute,
} from '@tanstack/react-router'
import { Toaster } from '@/components/ui/sonner'

import '../styles.css'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      {
        charSet: 'utf-8',
      },
      {
        name: 'viewport',
        content: 'width=device-width, initial-scale=1',
      },
      {
        title: 'WebRTC Gateway — SIP/WebRTC Console',
      },
      {
        name: 'description',
        content:
          'WebRTC Gateway test console for SIP and WebRTC calls, messaging, and trunk management.',
      },
    ],
  }),

  shellComponent: RootDocument,
  component: RootLayout,
})

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <head>
        <HeadContent />
        <script
          dangerouslySetInnerHTML={{
            __html: `(function(){try{var r=localStorage.getItem('k2-theme');if(!r)return;var e=JSON.parse(r);if(e&&e.data==='light')document.documentElement.classList.remove('dark')}catch(x){}})()`,
          }}
        />
      </head>
      <body className="overflow-hidden" suppressHydrationWarning>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function RootLayout() {
  return (
    <>
      <Outlet />
      <Toaster />
    </>
  )
}
