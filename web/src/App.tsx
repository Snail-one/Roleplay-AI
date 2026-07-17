import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { ApiError, chat, checkHealth, resetSession } from './api'

type Role = 'user' | 'assistant'

interface Message {
  id: string
  role: Role
  content: string
}

type ConnectionState = 'checking' | 'online' | 'offline'

const messagesStorageKey = 'roleloom.messages'
const sessionStorageKey = 'roleloom.session-id'

const suggestions = [
  '帮我规划今天最重要的三件事',
  '解释一个我最近没弄懂的概念',
  '计算 (128 + 72) ÷ 5',
]

function loadMessages(): Message[] {
  try {
    const value = localStorage.getItem(messagesStorageKey)
    if (!value) return []
    const parsed = JSON.parse(value) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter(
      (item): item is Message =>
        typeof item === 'object' &&
        item !== null &&
        'id' in item &&
        'role' in item &&
        'content' in item &&
        typeof item.id === 'string' &&
        (item.role === 'user' || item.role === 'assistant') &&
        typeof item.content === 'string',
    ).slice(-100)
  } catch {
    return []
  }
}

function createID(): string {
  return typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function BrandMark({ small = false }: { small?: boolean }) {
  return (
    <span className={small ? 'brand-mark brand-mark--small' : 'brand-mark'} aria-hidden="true">
      <svg viewBox="0 0 32 32" role="img">
        <path d="M9 8.5h9.2c3.5 0 5.8 2 5.8 5 0 2.5-1.2 4.2-3.3 4.9L25 24h-5.2l-3.6-5H14v5H9V8.5Zm5 4v3h3.6c1 0 1.6-.5 1.6-1.5s-.6-1.5-1.6-1.5H14Z" />
      </svg>
    </span>
  )
}

function SendIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="m5 12 14-7-4.8 14-2.7-5.5L5 12Z" />
      <path d="m11.5 13.5 3-3" />
    </svg>
  )
}

function ResetIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M4.8 9A8 8 0 1 1 4 14" />
      <path d="M4.8 4v5h5" />
    </svg>
  )
}

function App() {
  const [messages, setMessages] = useState<Message[]>(loadMessages)
  const [sessionID, setSessionID] = useState<string | null>(() => localStorage.getItem(sessionStorageKey))
  const [input, setInput] = useState('')
  const [isSending, setIsSending] = useState(false)
  const [isResetting, setIsResetting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [connection, setConnection] = useState<ConnectionState>('checking')
  const endRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    localStorage.setItem(messagesStorageKey, JSON.stringify(messages.slice(-100)))
    endRef.current?.scrollIntoView({ behavior: messages.length > 1 ? 'smooth' : 'auto' })
  }, [messages, isSending])

  useEffect(() => {
    const controller = new AbortController()
    void checkHealth(controller.signal).then((healthy) => setConnection(healthy ? 'online' : 'offline'))
    return () => controller.abort()
  }, [])

  useEffect(() => {
    const textarea = textareaRef.current
    if (!textarea) return
    textarea.style.height = '0px'
    textarea.style.height = `${Math.min(textarea.scrollHeight, 160)}px`
  }, [input])

  async function sendMessage(value = input) {
    const content = value.trim()
    if (!content || isSending) return

    const userMessage: Message = { id: createID(), role: 'user', content }
    setMessages((current) => [...current, userMessage])
    setInput('')
    setError(null)
    setIsSending(true)
    try {
      const response = await chat(content, sessionID)
      setSessionID(response.session_id)
      localStorage.setItem(sessionStorageKey, response.session_id)
      setMessages((current) => [
        ...current,
        { id: createID(), role: 'assistant', content: response.answer },
      ])
      setConnection('online')
    } catch (requestError) {
      setError(requestError instanceof ApiError ? requestError.message : '无法连接到后端服务')
      setConnection('offline')
    } finally {
      setIsSending(false)
      requestAnimationFrame(() => textareaRef.current?.focus())
    }
  }

  async function startNewConversation() {
    if (isSending || isResetting) return
    setIsResetting(true)
    setError(null)
    try {
      await resetSession(sessionID)
      setMessages([])
      setSessionID(null)
      localStorage.removeItem(sessionStorageKey)
      localStorage.removeItem(messagesStorageKey)
    } catch (requestError) {
      setError(requestError instanceof ApiError ? requestError.message : '无法清空当前会话')
    } finally {
      setIsResetting(false)
    }
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      void sendMessage()
    }
  }

  const isEmpty = messages.length === 0
  const canSend = input.trim().length > 0 && !isSending

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="topbar__inner">
          <a className="brand" href="/" aria-label="RoleLoom 首页">
            <BrandMark small />
            <span>RoleLoom</span>
          </a>
          <div className="topbar__actions">
            <span className={`connection connection--${connection}`} title="后端连接状态">
              <span className="connection__dot" />
              {connection === 'online' ? '已连接' : connection === 'offline' ? '未连接' : '检查中'}
            </span>
            <button
              className="reset-button"
              type="button"
              onClick={() => void startNewConversation()}
              disabled={isResetting || isSending}
              aria-label="开始新对话"
            >
              <ResetIcon />
              <span>新对话</span>
            </button>
          </div>
        </div>
      </header>

      <main className={`chat ${isEmpty ? 'chat--empty' : ''}`}>
        {isEmpty ? (
          <section className="welcome" aria-labelledby="welcome-title">
            <BrandMark />
            <p className="welcome__eyebrow">AI ASSISTANT</p>
            <h1 id="welcome-title">今天想聊点什么？</h1>
            <p className="welcome__description">提问、梳理想法，或让 Agent 使用工具完成简单任务。</p>
            <div className="suggestions" aria-label="建议问题">
              {suggestions.map((suggestion) => (
                <button key={suggestion} type="button" onClick={() => void sendMessage(suggestion)}>
                  {suggestion}
                  <span aria-hidden="true">↗</span>
                </button>
              ))}
            </div>
          </section>
        ) : (
          <section className="messages" aria-live="polite" aria-label="聊天记录">
            {messages.map((message) => (
              <article key={message.id} className={`message message--${message.role}`}>
                {message.role === 'assistant' && <BrandMark small />}
                <div className="message__body">
                  <span className="message__label">{message.role === 'user' ? '你' : 'RoleLoom'}</span>
                  <p>{message.content}</p>
                </div>
              </article>
            ))}
            {isSending && (
              <article className="message message--assistant" aria-label="AI 正在回复">
                <BrandMark small />
                <div className="typing" aria-hidden="true">
                  <span />
                  <span />
                  <span />
                </div>
              </article>
            )}
            <div ref={endRef} />
          </section>
        )}
      </main>

      <footer className="composer-area">
        <div className="composer-wrap">
          {error && (
            <div className="error-banner" role="alert">
              <span>{error}</span>
              <button type="button" onClick={() => setError(null)} aria-label="关闭错误提示">×</button>
            </div>
          )}
          <div className="composer">
            <textarea
              ref={textareaRef}
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="给 RoleLoom 发送消息…"
              rows={1}
              maxLength={32000}
              disabled={isSending}
              aria-label="消息"
            />
            <button
              className="send-button"
              type="button"
              onClick={() => void sendMessage()}
              disabled={!canSend}
              aria-label="发送消息"
            >
              <SendIcon />
            </button>
          </div>
          <p className="composer-hint">Enter 发送 · Shift + Enter 换行 · AI 可能会犯错，请核对重要信息</p>
        </div>
      </footer>
    </div>
  )
}

export default App
