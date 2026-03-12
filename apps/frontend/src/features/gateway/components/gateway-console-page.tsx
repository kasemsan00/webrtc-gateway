import { useStore } from '@tanstack/react-store'
import {
  RiArrowDownSLine,
  RiArrowRightSLine,
  RiFullscreenExitLine,
  RiFullscreenLine,
  RiMicLine,
  RiMicOffLine,
  RiMoonLine,
  RiPhoneLine,
  RiSendPlaneLine,
  RiShutDownLine,
  RiSignalWifiLine,
  RiSunLine,
  RiVideoOffLine,
  RiVideoOnLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import Header from '@/components/Header'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useTheme } from '@/lib/theme'
import { fetchTrunks } from '@/features/trunk/services/trunk-api'
import { normalizeTrunkUid, type Trunk } from '@/features/trunk/types'
import {
  canPlaceCall,
  canResolveTrunk,
  gatewayActions,
  gatewayStore,
  isCallInProgress,
} from '@/features/gateway/store/gateway-store'

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

function toTimer(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

function callBadgeVariant(
  s: string,
): 'outline' | 'success' | 'warning' | 'destructive' {
  if (s === 'active') return 'success'
  if (s === 'connecting' || s === 'ringing' || s === 'reconnecting')
    return 'warning'
  if (s === 'ended') return 'destructive'
  return 'outline'
}

function logPrefix(type: string) {
  if (type === 'error') return '[ERR] '
  if (type === 'warning') return '[WARN] '
  if (type === 'success') return '[OK] '
  return ''
}

function parsePortInput(raw: string, fallback: number) {
  const parsed = Number.parseInt(raw, 10)
  if (!Number.isInteger(parsed)) return fallback
  return Math.max(0, Math.min(65535, parsed))
}

/* ------------------------------------------------------------------ */
/*  Tiny reusable bits                                                 */
/* ------------------------------------------------------------------ */

function Field({
  label,
  id,
  children,
}: {
  label: string
  id: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-0.5">
      <label
        htmlFor={id}
        className="text-[11px] font-medium text-muted-foreground"
      >
        {label}
      </label>
      {children}
    </div>
  )
}

function RadioOption({
  label,
  checked,
  name,
  onChange,
}: {
  label: string
  checked: boolean
  name: string
  onChange: () => void
}) {
  return (
    <label
      className={`flex cursor-pointer items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs transition-colors ${
        checked
          ? 'border-cyan-500/40 bg-cyan-500/10 text-cyan-700 dark:text-cyan-200'
          : 'border-border text-muted-foreground hover:border-muted-foreground/40'
      }`}
    >
      <span
        className={`flex size-3 shrink-0 items-center justify-center rounded-full border-[1.5px] ${
          checked ? 'border-cyan-400' : 'border-muted-foreground'
        }`}
      >
        {checked ? (
          <span className="size-1.5 rounded-full bg-cyan-400" />
        ) : null}
      </span>
      <input
        type="radio"
        name={name}
        checked={checked}
        onChange={onChange}
        className="sr-only"
      />
      <span className="font-medium">{label}</span>
    </label>
  )
}

function useAutoScroll(dep: unknown) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [dep])
  return ref
}

/* ------------------------------------------------------------------ */
/*  Page                                                               */
/* ------------------------------------------------------------------ */

export function GatewayConsolePage() {
  const state = useStore(gatewayStore, (storeState) => storeState)
  const { theme, toggleTheme } = useTheme()
  const [messageBody, setMessageBody] = useState('')
  const [isSendingSwitch, setIsSendingSwitch] = useState(false)
  const [trunkOptions, setTrunkOptions] = useState<Array<{
    value: string
    label: string
  }>>([])
  const [trunkOptionsLoading, setTrunkOptionsLoading] = useState(false)
  const [trunkOptionsError, setTrunkOptionsError] = useState('')

  const localVideoRef = useRef<HTMLVideoElement>(null)
  const remoteVideoRef = useRef<HTMLVideoElement>(null)
  const remoteAudioRef = useRef<HTMLAudioElement>(null)
  const acceptBtnRef = useRef<HTMLButtonElement>(null)
  const remotePanelRef = useRef<HTMLElement>(null)

  const callEnabled = useMemo(() => canPlaceCall(state), [state])
  const trunkResolveEnabled = useMemo(() => canResolveTrunk(state), [state])
  const inCall = useMemo(() => isCallInProgress(state), [state])
  const [isRemoteFullscreen, setIsRemoteFullscreen] = useState(false)

  const logsRef = useAutoScroll(state.logs)
  const msgsRef = useAutoScroll(state.messages)

  const hasRemoteVideo = Boolean(state.media.remoteVideoStream)
  const hasLocalVideo = Boolean(state.media.localStream)

  const selectedCameraValue =
    state.controls.selectedVideoInputId || '__default__'
  const selectedMicValue = state.controls.selectedAudioInputId || '__default__'
  const incomingBusy = state.incomingAction !== 'idle'
  const incomingBusyLabel =
    state.incomingAction === 'preparing_accept'
      ? 'Preparing local media session...'
      : state.incomingAction === 'sending_accept'
        ? 'Sending accept...'
        : state.incomingAction === 'sending_reject'
          ? 'Sending reject...'
          : ''

  // Init store
  useEffect(() => {
    gatewayActions.initialize()

    const connectTimer = setTimeout(() => {
      if (
        gatewayStore.state.connection.status !== 'connected' &&
        gatewayStore.state.connection.status !== 'connecting' &&
        gatewayStore.state.connection.status !== 'reconnecting'
      ) {
        gatewayActions.connect()
      }
    }, 0)

    return () => {
      clearTimeout(connectTimer)
      gatewayActions.cleanup()
    }
  }, [])

  // Bind streams
  useEffect(() => {
    if (localVideoRef.current)
      localVideoRef.current.srcObject = state.media.localStream
  }, [state.media.localStream])
  useEffect(() => {
    if (remoteVideoRef.current) {
      remoteVideoRef.current.srcObject = state.media.remoteVideoStream
    }
  }, [state.media.remoteVideoStream])
  useEffect(() => {
    if (remoteAudioRef.current)
      remoteAudioRef.current.srcObject = state.media.remoteAudioStream
  }, [state.media.remoteAudioStream])

  // Incoming call: focus + Esc
  useEffect(() => {
    if (state.incomingCall) acceptBtnRef.current?.focus()
  }, [state.incomingCall])
  useEffect(() => {
    if (!state.incomingCall) return
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') gatewayActions.rejectCall()
    }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [state.incomingCall])

  useEffect(() => {
    const onFullscreenChange = () => {
      const panel = remotePanelRef.current
      setIsRemoteFullscreen(
        Boolean(panel && document.fullscreenElement === panel),
      )
    }

    document.addEventListener('fullscreenchange', onFullscreenChange)
    return () => {
      document.removeEventListener('fullscreenchange', onFullscreenChange)
    }
  }, [])

  const handleToggleRemoteFullscreen = useCallback(async () => {
    const panel = remotePanelRef.current
    if (!panel || !document.fullscreenEnabled) return

    try {
      if (document.fullscreenElement === panel) {
        await document.exitFullscreen()
      } else {
        await panel.requestFullscreen()
      }
    } catch {
      // Ignore fullscreen failures caused by browser restrictions.
    }
  }, [])

  const handleSendMessage = useCallback(() => {
    if (gatewayActions.sendSIPMessage(messageBody)) setMessageBody('')
  }, [messageBody])

  const handleSendSwitch = useCallback(async () => {
    if (isSendingSwitch) return
    setIsSendingSwitch(true)
    try {
      await gatewayActions.sendSwitch()
    } finally {
      setIsSendingSwitch(false)
    }
  }, [isSendingSwitch])

  const onExtKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' && callEnabled) {
        e.preventDefault()
        gatewayActions.makeCall()
      }
    },
    [callEnabled],
  )

  const onMsgKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        handleSendMessage()
      }
    },
    [handleSendMessage],
  )

  const loadTrunkOptions = useCallback(async () => {
    setTrunkOptionsLoading(true)
    setTrunkOptionsError('')
    try {
      const res = await fetchTrunks({
        page: 1,
        pageSize: 200,
        sortBy: 'id',
        sortDir: 'asc',
      })
      const items = res.items.filter((trunk) => trunk.enabled)
      const options = items.map((trunk: Trunk) => {
        const publicId = normalizeTrunkUid(trunk)
        return {
          value: publicId || String(trunk.id),
          label: `${trunk.name} (#${trunk.id}${publicId ? ` · uid: ${publicId}` : ''})`,
        }
      })
      setTrunkOptions(options)
    } catch (error) {
      setTrunkOptions([])
      setTrunkOptionsError(
        error instanceof Error ? error.message : 'Failed to fetch trunks',
      )
    } finally {
      setTrunkOptionsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (state.mode !== 'siptrunk') return
    void loadTrunkOptions()
  }, [loadTrunkOptions, state.mode])

  /* ---------------------------------------------------------------- */
  /*  Render                                                           */
  /* ---------------------------------------------------------------- */

  const selectedTrunkIdentifier = state.trunk.credentials.trunkId.trim()
  const selectedTrunkValue = useMemo(() => {
    if (!selectedTrunkIdentifier) return '__none__'
    if (trunkOptionsLoading) return selectedTrunkIdentifier
    return trunkOptions.some((option) => option.value === selectedTrunkIdentifier)
      ? selectedTrunkIdentifier
      : '__none__'
  }, [selectedTrunkIdentifier, trunkOptions, trunkOptionsLoading])
  const resolveTrunkDisabled =
    !trunkResolveEnabled ||
    !selectedTrunkIdentifier ||
    trunkOptionsLoading ||
    state.trunk.status === 'resolving' ||
    state.trunk.status === 'redirecting'

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {/* Connection status */}
          <div
            className="flex items-center gap-1.5"
            role="status"
            aria-live="polite"
          >
            <span
              aria-hidden="true"
              className={`size-2 rounded-full ${
                state.connection.status === 'connected'
                  ? 'bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.6)]'
                  : state.connection.status === 'connecting' ||
                      state.connection.status === 'reconnecting'
                    ? 'animate-pulse bg-amber-300'
                    : 'bg-red-400'
              }`}
            />
            <span>{state.connection.wsStateText}</span>
          </div>
          <Separator orientation="vertical" className="h-4" />
          {/* Media status */}
          <span>{state.media.rtcStateText}</span>
          <Separator orientation="vertical" className="h-4" />
          {/* Call status */}
          <div
            className="flex items-center gap-1.5"
            role="status"
            aria-live="polite"
          >
            <span className="font-mono">
              {toTimer(state.call.elapsedSeconds)}
            </span>
            <Badge variant={callBadgeVariant(state.call.state)}>
              {state.call.state.toUpperCase()}
            </Badge>
          </div>
          <Separator orientation="vertical" className="h-4" />
          {/* Theme toggle */}
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            onClick={toggleTheme}
            aria-label={
              theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'
            }
          >
            {theme === 'dark' ? (
              <RiSunLine className="size-3.5" />
            ) : (
              <RiMoonLine className="size-3.5" />
            )}
          </Button>
        </div>
      </Header>

      {/* ===== MAIN 3-COL LAYOUT ===== */}
      <div className="grid min-h-0 flex-1 grid-cols-[280px_1fr_300px] overflow-hidden">
        {/* ---- LEFT COLUMN: Controls ---- */}
        <aside className="flex min-h-0 min-w-0 flex-col gap-2 overflow-y-auto border-r border-border p-2">
          {/* Connection */}
          <Card className="gap-0 py-0">
            <CardContent className="space-y-2 p-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">Signaling</span>
                <Button
                  size="sm"
                  variant={
                    state.connection.status === 'connected'
                      ? 'destructive'
                      : 'default'
                  }
                  onClick={gatewayActions.toggleConnect}
                  disabled={
                    state.connection.status === 'connecting' ||
                    state.connection.status === 'reconnecting'
                  }
                  className="h-6 px-2 text-[11px]"
                >
                  {state.connection.status === 'connecting'
                    ? 'Connecting...'
                    : state.connection.status === 'reconnecting'
                      ? 'Reconnecting...'
                      : state.connection.status === 'connected'
                        ? 'Disconnect'
                        : 'Connect'}
                </Button>
              </div>
              <Separator />
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">Media</span>
                <Button
                  size="sm"
                  variant={
                    state.media.status === 'active' ? 'destructive' : 'outline'
                  }
                  onClick={
                    state.media.status === 'active'
                      ? gatewayActions.endSession
                      : gatewayActions.startSession
                  }
                  disabled={state.connection.status !== 'connected'}
                  className="h-6 px-2 text-[11px]"
                >
                  {state.media.status === 'active' ? 'End' : 'Start'}
                </Button>
              </div>
              <div className="grid grid-cols-1 gap-1.5">
                <Field label="Camera" id="camera-input-select">
                  <Select
                    value={selectedCameraValue}
                    onValueChange={(value) => {
                      void gatewayActions.setSelectedVideoInput(value)
                    }}
                    disabled={
                      state.controls.mediaInputsLoading ||
                      state.controls.switchingVideoInput
                    }
                  >
                    <SelectTrigger
                      id="camera-input-select"
                      className="h-7 w-full px-2 text-xs"
                      size="sm"
                    >
                      <SelectValue placeholder="Default camera" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__default__">
                        Default camera
                      </SelectItem>
                      {state.controls.availableVideoInputs.map((device) => (
                        <SelectItem
                          key={device.deviceId}
                          value={device.deviceId}
                        >
                          {device.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="Microphone" id="microphone-input-select">
                  <Select
                    value={selectedMicValue}
                    onValueChange={(value) => {
                      void gatewayActions.setSelectedAudioInput(value)
                    }}
                    disabled={
                      state.controls.mediaInputsLoading ||
                      state.controls.switchingAudioInput
                    }
                  >
                    <SelectTrigger
                      id="microphone-input-select"
                      className="h-7 w-full px-2 text-xs"
                      size="sm"
                    >
                      <SelectValue placeholder="Default microphone" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__default__">
                        Default microphone
                      </SelectItem>
                      {state.controls.availableAudioInputs.map((device) => (
                        <SelectItem
                          key={device.deviceId}
                          value={device.deviceId}
                        >
                          {device.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              </div>
            </CardContent>
          </Card>

          {/* Call Mode */}
          <Card className="gap-0 py-0">
            <CardContent className="p-3">
              <p className="mb-1.5 text-[11px] font-medium text-muted-foreground">
                Mode
              </p>
              <div className="grid grid-cols-3 gap-1.5">
                <RadioOption
                  label="SIP"
                  name="mode"
                  checked={state.mode === 'public'}
                  onChange={() => gatewayActions.setMode('public')}
                />
                <RadioOption
                  label="VRI"
                  name="mode"
                  checked={state.mode === 'publicvrs'}
                  onChange={() => gatewayActions.setMode('publicvrs')}
                />
                <RadioOption
                  label="Trunk"
                  name="mode"
                  checked={state.mode === 'siptrunk'}
                  onChange={() => gatewayActions.setMode('siptrunk')}
                />
              </div>
            </CardContent>
          </Card>

          {/* Credentials */}
          {state.mode === 'public' ? (
            <Card className="gap-0 py-0">
              <CardHeader className="px-3 py-2 pb-0">
                <CardTitle className="text-xs">SIP Credentials</CardTitle>
              </CardHeader>
              <CardContent className="space-y-1.5 p-3 pt-1.5">
                <Field label="Domain" id="pub-d">
                  <Input
                    id="pub-d"
                    className="h-7 text-xs"
                    placeholder="sip.example.com"
                    value={state.publicCredentials.sipDomain}
                    onChange={(e) =>
                      gatewayActions.setPublicCredential(
                        'sipDomain',
                        e.target.value,
                      )
                    }
                  />
                </Field>
                <Field label="Username" id="pub-u">
                  <Input
                    id="pub-u"
                    className="h-7 text-xs"
                    placeholder="1001"
                    value={state.publicCredentials.sipUsername}
                    onChange={(e) =>
                      gatewayActions.setPublicCredential(
                        'sipUsername',
                        e.target.value,
                      )
                    }
                  />
                </Field>
                <Field label="Password" id="pub-p">
                  <Input
                    id="pub-p"
                    className="h-7 text-xs"
                    type="password"
                    placeholder="********"
                    value={state.publicCredentials.sipPassword}
                    onChange={(e) =>
                      gatewayActions.setPublicCredential(
                        'sipPassword',
                        e.target.value,
                      )
                    }
                  />
                </Field>
                <Field label="Port" id="pub-port">
                  <Input
                    id="pub-port"
                    className="h-7 text-xs"
                    type="number"
                    min={0}
                    max={65535}
                    placeholder="5060 (0 = SRV)"
                    value={state.publicCredentials.sipPort}
                    onChange={(e) =>
                      gatewayActions.setPublicCredential(
                        'sipPort',
                        parsePortInput(e.target.value, 5060),
                      )
                    }
                  />
                </Field>
              </CardContent>
            </Card>
          ) : state.mode === 'publicvrs' ? (
            <Card className="gap-0 py-0">
              <CardHeader className="px-3 py-2 pb-0">
                <CardTitle className="flex items-center text-xs">
                  VRI
                  <Badge
                    variant={
                      state.vrs.fetchStatus === 'fetched'
                        ? 'success'
                        : state.vrs.fetchStatus === 'fetching'
                          ? 'warning'
                          : state.vrs.fetchStatus === 'error'
                            ? 'destructive'
                            : 'outline'
                    }
                    className="ml-2 text-[10px]"
                  >
                    {state.vrs.fetchStatus}
                  </Badge>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-1.5 p-3 pt-1.5">
                <Field label="Phone" id="vrs-phone">
                  <Input
                    id="vrs-phone"
                    className="h-7 text-xs"
                    placeholder="0828955697"
                    value={state.vrs.config.phone}
                    onChange={(e) =>
                      gatewayActions.setVrsConfigField('phone', e.target.value)
                    }
                  />
                </Field>
                <Field label="Full Name" id="vrs-name">
                  <Input
                    id="vrs-name"
                    className="h-7 text-xs"
                    placeholder="Example"
                    value={state.vrs.config.fullName}
                    onChange={(e) =>
                      gatewayActions.setVrsConfigField(
                        'fullName',
                        e.target.value,
                      )
                    }
                  />
                </Field>
                {state.vrs.resolvedCredentials ? (
                  <div className="rounded-md border border-emerald-500/20 bg-emerald-500/5 p-2 text-[10px] text-muted-foreground">
                    <p>
                      <span className="font-medium">Ext:</span>{' '}
                      {state.vrs.resolvedCredentials.sipUsername}
                    </p>
                    <p>
                      <span className="font-medium">Domain:</span>{' '}
                      {state.vrs.resolvedCredentials.sipDomain}
                    </p>
                  </div>
                ) : null}
              </CardContent>
            </Card>
          ) : (
            <Card className="gap-0 py-0">
              <CardHeader className="px-3 py-2 pb-0">
                <CardTitle className="flex items-center text-xs">
                  Trunk
                  <Badge
                    variant={
                      state.trunk.status === 'resolved'
                        ? 'success'
                        : state.trunk.status === 'resolving' ||
                            state.trunk.status === 'redirecting'
                          ? 'warning'
                          : 'destructive'
                    }
                    className="ml-2 text-[10px]"
                  >
                    {state.trunk.status}
                  </Badge>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-1.5 p-3 pt-1.5">
                <Field label="Trunk ID / UUID" id="t-id">
                  <Select
                    value={selectedTrunkValue}
                    onValueChange={(value) =>
                      gatewayActions.setTrunkCredential(
                        'trunkId',
                        value === '__none__' ? '' : value,
                      )
                    }
                    disabled={trunkOptionsLoading}
                  >
                    <SelectTrigger id="t-id" className="h-7 w-full px-2 text-xs" size="sm">
                      <SelectValue
                        placeholder={
                          trunkOptionsLoading ? 'Loading trunks...' : 'Select trunk'
                        }
                      />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__none__">Select trunk</SelectItem>
                      {trunkOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                {trunkOptionsError ? (
                  <p className="text-[10px] text-red-600 dark:text-red-300">
                    Failed to load trunks: {trunkOptionsError}
                  </p>
                ) : null}
                {!trunkOptionsLoading && !trunkOptionsError && trunkOptions.length === 0 ? (
                  <p className="text-[10px] text-muted-foreground">
                    No enabled trunks available
                  </p>
                ) : null}
                <Button
                  variant="outline"
                  className="h-7 w-full text-[11px]"
                  onClick={gatewayActions.resolveTrunk}
                  disabled={resolveTrunkDisabled}
                >
                  {trunkOptionsLoading
                    ? 'Loading trunks...'
                    : state.trunk.status === 'resolving'
                    ? 'Resolving...'
                    : 'Resolve'}
                </Button>
              </CardContent>
            </Card>
          )}

          {/* Call Controls */}
          <Card className="gap-0 py-0">
            <CardContent className="space-y-2 p-3">
              <Field label="Destination" id="dest">
                <Input
                  id="dest"
                  className="h-7 text-xs"
                  placeholder="9999"
                  value={state.controls.destination}
                  onChange={(e) =>
                    gatewayActions.setDestination(e.target.value)
                  }
                  onKeyDown={onExtKeyDown}
                />
              </Field>
              <div className="flex gap-1.5">
                {['9999', '*97', '100'].map((p) => (
                  <Button
                    key={p}
                    size="sm"
                    variant="outline"
                    className="h-6 flex-1 px-1 text-[11px]"
                    onClick={() => gatewayActions.setInputPreset(p)}
                  >
                    {p}
                  </Button>
                ))}
              </div>
              <div className="grid grid-cols-2 gap-1.5">
                <Button
                  className="h-7 text-xs"
                  onClick={gatewayActions.makeCall}
                  disabled={!callEnabled}
                >
                  <RiPhoneLine className="size-3" /> Call
                </Button>
                <Button
                  variant="destructive"
                  className="h-7 text-xs"
                  onClick={gatewayActions.hangup}
                  disabled={!inCall}
                >
                  <RiShutDownLine className="size-3" /> Hangup
                </Button>
              </div>
              <Button
                variant="outline"
                className="h-6 w-full text-[11px]"
                onClick={gatewayActions.toggleDialpad}
                aria-expanded={state.controls.dialpadOpen}
              >
                {state.controls.dialpadOpen ? (
                  <>
                    <RiArrowDownSLine className="size-3" /> Dialpad
                  </>
                ) : (
                  <>
                    <RiArrowRightSLine className="size-3" /> Dialpad
                  </>
                )}
              </Button>
              {state.controls.dialpadOpen ? (
                <div className="grid grid-cols-3 gap-1">
                  {[
                    '1',
                    '2',
                    '3',
                    '4',
                    '5',
                    '6',
                    '7',
                    '8',
                    '9',
                    '*',
                    '0',
                    '#',
                  ].map((d) => (
                    <Button
                      key={d}
                      variant="outline"
                      className="h-7 text-xs"
                      onClick={() => gatewayActions.sendDTMF(d)}
                    >
                      {d}
                    </Button>
                  ))}
                </div>
              ) : null}

              <Separator />
              <div className="space-y-1.5">
                <p className="text-[11px] font-medium text-muted-foreground">
                  Switch Trigger
                </p>
                <Button
                  variant="outline"
                  className="h-7 w-full text-[11px]"
                  onClick={() => {
                    void handleSendSwitch()
                  }}
                  disabled={!state.call.sessionId || isSendingSwitch}
                >
                  {isSendingSwitch ? 'Sending...' : 'Send Switch'}
                </Button>
              </div>
            </CardContent>
          </Card>
        </aside>

        {/* ---- CENTER: Video ---- */}
        <main
          ref={remotePanelRef}
          className="relative flex min-h-0 min-w-0 flex-col overflow-hidden bg-black"
        >
          <video
            ref={remoteVideoRef}
            autoPlay
            playsInline
            muted
            aria-label="Remote video"
            className="h-full w-full object-contain"
          />

          {!hasRemoteVideo ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-gray-600">
              <RiVideoOnLine className="size-10 opacity-30" />
              <p className="text-sm">No active video call</p>
            </div>
          ) : null}

          {state.rtt.remotePreviewText ? (
            <div
              className="pointer-events-none absolute left-1/2 top-1/2 z-30 w-[min(85%,44rem)] -translate-x-1/2 -translate-y-1/2 rounded-lg bg-black/85 px-4 py-3 text-center text-sm text-white shadow-lg"
              aria-live="polite"
              role="status"
            >
              <p className="whitespace-pre-wrap wrap-break-word">
                {state.rtt.remotePreviewText}
              </p>
            </div>
          ) : null}

          {hasLocalVideo ? (
            <div className="absolute bottom-14 right-3 h-24 w-36 overflow-hidden rounded-lg border border-white/20 bg-gray-900">
              <video
                ref={localVideoRef}
                autoPlay
                playsInline
                muted
                aria-label="Local preview"
                className="h-full w-full scale-x-[-1] object-cover"
              />
            </div>
          ) : (
            <div className="hidden">
              <video
                ref={localVideoRef}
                autoPlay
                playsInline
                muted
                aria-label="Local preview"
              />
            </div>
          )}

          {/* Floating controls */}
          <div className="absolute bottom-3 left-1/2 flex -translate-x-1/2 flex-col items-center gap-1.5">
            <div className="flex items-center gap-1.5 rounded-full px-3 py-1.5 backdrop-blur">
              <Button
                size="icon"
                className="size-12"
                variant={
                  state.controls.isMutedAudio ? 'destructive' : 'outline'
                }
                onClick={gatewayActions.toggleMuteAudio}
                aria-label={
                  state.controls.isMutedAudio ? 'Unmute mic' : 'Mute mic'
                }
                aria-pressed={state.controls.isMutedAudio}
              >
                {state.controls.isMutedAudio ? (
                  <RiMicOffLine className="size-5" />
                ) : (
                  <RiMicLine className="size-5" />
                )}
              </Button>
              <Button
                size="icon"
                className="size-12"
                variant={
                  state.controls.isMutedVideo ? 'destructive' : 'outline'
                }
                onClick={gatewayActions.toggleMuteVideo}
                aria-label={
                  state.controls.isMutedVideo
                    ? 'Turn on camera'
                    : 'Turn off camera'
                }
                aria-pressed={state.controls.isMutedVideo}
              >
                {state.controls.isMutedVideo ? (
                  <RiVideoOffLine className="size-5" />
                ) : (
                  <RiVideoOnLine className="size-5" />
                )}
              </Button>
              <Button
                size="icon"
                className="size-12"
                variant="destructive"
                onClick={gatewayActions.hangup}
                disabled={!inCall}
                aria-label="Hang up"
              >
                <RiShutDownLine className="size-5" />
              </Button>
              <Button
                size="icon"
                className="size-12"
                variant={state.controls.statsOpen ? 'default' : 'outline'}
                onClick={gatewayActions.toggleStats}
                aria-label={
                  state.controls.statsOpen ? 'Hide stats' : 'Show stats'
                }
                aria-pressed={state.controls.statsOpen}
              >
                <RiSignalWifiLine className="size-5" />
              </Button>
            </div>
            {state.media.status === 'active' ? (
              <div className="flex items-center gap-1.5 rounded-full bg-black/50 px-2 py-1 backdrop-blur">
                <Select
                  value={selectedCameraValue}
                  onValueChange={(value) => {
                    void gatewayActions.setSelectedVideoInput(value)
                  }}
                  disabled={state.controls.switchingVideoInput}
                >
                  <SelectTrigger
                    className="h-8 w-44 bg-black/30 px-2 text-xs"
                    size="sm"
                  >
                    <SelectValue placeholder="Camera" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__default__">Default camera</SelectItem>
                    {state.controls.availableVideoInputs.map((device) => (
                      <SelectItem key={device.deviceId} value={device.deviceId}>
                        {device.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select
                  value={selectedMicValue}
                  onValueChange={(value) => {
                    void gatewayActions.setSelectedAudioInput(value)
                  }}
                  disabled={state.controls.switchingAudioInput}
                >
                  <SelectTrigger
                    className="h-8 w-44 bg-black/30 px-2 text-xs"
                    size="sm"
                  >
                    <SelectValue placeholder="Microphone" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__default__">
                      Default microphone
                    </SelectItem>
                    {state.controls.availableAudioInputs.map((device) => (
                      <SelectItem key={device.deviceId} value={device.deviceId}>
                        {device.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ) : null}
          </div>

          <Button
            size="icon"
            className="absolute right-3 top-3 z-20 size-9 rounded-full border-white/20 bg-black/50 text-white hover:bg-black/70"
            variant="outline"
            onClick={() => {
              void handleToggleRemoteFullscreen()
            }}
            disabled={!document.fullscreenEnabled}
            aria-label={
              isRemoteFullscreen ? 'Exit fullscreen' : 'Enter fullscreen'
            }
            aria-pressed={isRemoteFullscreen}
          >
            {isRemoteFullscreen ? (
              <RiFullscreenExitLine className="size-4" />
            ) : (
              <RiFullscreenLine className="size-4" />
            )}
          </Button>

          {state.controls.statsOpen ? (
            <div className="absolute left-3 top-3 rounded-md border border-white/10 bg-black/70 p-2 font-mono text-[11px] text-gray-300 backdrop-blur">
              <p>RTT: {state.stats.rttMs} ms</p>
              <p>Loss: {state.stats.packetLossPercent}%</p>
              <p>Bitrate: {state.stats.bitrateKbps} kbps</p>
              <p>Codec: {state.stats.codec}</p>
              <p>Res: {state.stats.resolution}</p>
            </div>
          ) : null}

          <audio ref={remoteAudioRef} autoPlay aria-label="Remote audio" />
        </main>

        {/* ---- RIGHT COLUMN: Logs + Messages ---- */}
        <aside className="flex min-h-0 min-w-0 flex-col overflow-hidden border-l border-border">
          {/* Logs (top half) */}
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex shrink-0 items-center justify-between border-b border-border px-3 py-1.5">
              <span className="text-xs font-semibold text-foreground/80">
                Logs
              </span>
              <div className="flex gap-1">
                <Button
                  size="sm"
                  variant="outline"
                  className="h-5 px-1.5 text-[10px]"
                  onClick={gatewayActions.sendPing}
                >
                  Ping
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-5 px-1.5 text-[10px]"
                  onClick={gatewayActions.clearLogs}
                >
                  Clear
                </Button>
              </div>
            </div>
            <div
              ref={logsRef}
              className="flex-1 overflow-y-auto p-2 font-mono text-[11px]"
              role="log"
              aria-live="polite"
              aria-label="Logs"
            >
              {state.logs.length === 0 ? (
                <p className="py-4 text-center text-xs text-muted-foreground/60">
                  No log entries yet.
                </p>
              ) : (
                <div className="space-y-0.5">
                  {state.logs.map((e) => (
                    <p
                      key={e.id}
                      className={`break-all leading-tight ${e.type === 'success' ? 'text-emerald-600 dark:text-emerald-300' : e.type === 'warning' ? 'text-amber-600 dark:text-amber-300' : e.type === 'error' ? 'text-red-600 dark:text-red-300' : 'text-cyan-600 dark:text-cyan-300'}`}
                    >
                      <span className="mr-1.5 text-muted-foreground/60">
                        {e.time}
                      </span>
                      <span className="text-muted-foreground/80">
                        {logPrefix(e.type)}
                      </span>
                      {e.message}
                    </p>
                  ))}
                </div>
              )}
            </div>
          </div>

          {/* Messages (bottom half) */}
          <div className="flex min-h-0 flex-1 flex-col border-t border-border">
            <div className="flex shrink-0 items-center justify-between border-b border-border px-3 py-1.5">
              <span className="text-xs font-semibold text-foreground/80">
                Messages
              </span>
              <Button
                size="sm"
                variant="ghost"
                className="h-5 px-1.5 text-[10px]"
                onClick={gatewayActions.clearMessages}
              >
                Clear
              </Button>
            </div>
            <div
              ref={msgsRef}
              className="flex-1 overflow-y-auto p-2 font-mono text-[11px]"
              role="log"
              aria-live="polite"
              aria-label="Messages"
            >
              {state.messages.length === 0 ? (
                <p className="py-4 text-center text-xs text-muted-foreground/60">
                  No messages yet.
                </p>
              ) : (
                <div className="space-y-0.5">
                  {state.messages.map((e) => (
                    <p
                      key={e.id}
                      className={`break-all leading-tight ${e.direction === 'incoming' ? 'text-emerald-600 dark:text-emerald-300' : 'text-cyan-600 dark:text-cyan-300'}`}
                    >
                      <span className="mr-1.5 text-muted-foreground/60">
                        {e.time}
                      </span>
                      <span className="font-semibold">{e.from}:</span> {e.body}
                    </p>
                  ))}
                </div>
              )}
            </div>
            {/* Compose */}
            <div className="shrink-0 border-t border-border p-2">
              <Textarea
                id="sip-msg"
                rows={1}
                className="mb-1.5 min-h-0 resize-none py-1.5 text-xs"
                placeholder="Enter to send, Shift+Enter newline"
                value={messageBody}
                onChange={(e) => {
                  const next = e.target.value
                  setMessageBody(next)
                  gatewayActions.updateOutgoingRttDraft(next)
                }}
                onKeyDown={onMsgKeyDown}
              />
              <Button
                className="h-6 w-full text-[11px]"
                onClick={handleSendMessage}
                disabled={state.connection.status !== 'connected'}
              >
                <RiSendPlaneLine className="size-3" /> Send
              </Button>
            </div>
          </div>

          {/* Session footer */}
          <div className="shrink-0 border-t border-border px-3 py-1.5 font-mono text-[10px] text-muted-foreground">
            <div className="flex flex-wrap gap-x-3 gap-y-0.5">
              <span>Session: {state.call.sessionId ?? '-'}</span>
              <span>ICE: {state.media.iceState}</span>
              <span>Sig: {state.media.signalingState}</span>
              <span>Calls: {state.call.callCount}</span>
            </div>
          </div>
        </aside>
      </div>

      {/* ===== INCOMING CALL MODAL ===== */}
      {state.incomingCall ? (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
          role="alertdialog"
          aria-modal="true"
          aria-labelledby="ic-title"
        >
          <Card className="w-full max-w-sm border-cyan-400/20 shadow-2xl">
            <CardHeader className="flex-col items-start gap-1 px-4 py-3">
              <CardTitle
                id="ic-title"
                className="text-lg text-cyan-700 dark:text-cyan-200"
              >
                Incoming Call
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 px-4 pb-4 pt-0">
              <p className="text-sm">
                From:{' '}
                <span className="font-semibold">{state.incomingCall.from}</span>
              </p>
              <p className="text-sm">
                To:{' '}
                <span className="font-semibold">{state.incomingCall.to}</span>
              </p>
              <p className="text-xs uppercase tracking-wide text-muted-foreground">
                Mode: {state.incomingCall.mode}
              </p>
              {incomingBusy ? (
                <p className="text-xs text-amber-600 dark:text-amber-300">
                  {incomingBusyLabel}
                </p>
              ) : null}
              <div className="grid grid-cols-2 gap-2 pt-1">
                <Button
                  variant="destructive"
                  className="h-8"
                  onClick={gatewayActions.rejectCall}
                  disabled={incomingBusy}
                >
                  Reject
                </Button>
                <Button
                  ref={acceptBtnRef}
                  className="h-8"
                  onClick={gatewayActions.acceptCall}
                  disabled={incomingBusy}
                >
                  Accept
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      ) : null}
    </div>
  )
}
