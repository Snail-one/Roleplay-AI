import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import App from './App'

function json(value:unknown,status=200){return new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})}

describe('App',()=>{
  it('shows login when the admin session is absent',async()=>{
    vi.stubGlobal('fetch',vi.fn(async()=>json({error:{message:'请先登录'}},401)))
    render(<App/>)
    expect(await screen.findByRole('heading',{name:'欢迎回到 RoleLoom'})).toBeTruthy()
    expect(screen.getByLabelText('管理员密码')).toBeTruthy()
  })

  it('shows only the API key presence flag in model settings',async()=>{
    const model={id:'model-1',name:'主模型',provider:'openai',api_url:'https://example.test/v1/chat/completions',has_api_key:true,model:'gpt-test',timeout_seconds:60,max_output_tokens:4096,context_window:32768,is_default:true}
    vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL)=>{const path=String(input);if(path.endsWith('/api/auth/session'))return json({authenticated:true});if(path.endsWith('/api/model-profiles'))return json([model]);if(path.endsWith('/api/characters'))return json([]);if(path.endsWith('/api/conversations'))return json([]);return json({},404)}))
    render(<App/>)
    fireEvent.click(await screen.findByRole('button',{name:'模型档案'}))
    expect(await screen.findByText((_,element)=>element?.tagName==='SMALL'&&element.textContent?.includes('密钥已保存')===true)).toBeTruthy()
    expect(document.body.textContent).not.toContain('top-secret')
    fireEvent.click(screen.getByRole('button',{name:'编辑'}))
    expect((screen.getByLabelText('新 API Key（留空则保留）') as HTMLInputElement).value).toBe('')
  })

  it('logs in and loads the database-backed workspace',async()=>{
    const fetchMock=vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{const path=String(input);if(path.endsWith('/api/auth/session'))return json({},401);if(path.endsWith('/api/auth/login')&&init?.method==='POST')return json({authenticated:true});if(path.endsWith('/api/model-profiles')||path.endsWith('/api/characters')||path.endsWith('/api/conversations'))return json([]);return json({},404)})
    vi.stubGlobal('fetch',fetchMock);render(<App/>);const password=await screen.findByLabelText('管理员密码');fireEvent.change(password,{target:{value:'a secure password'}});fireEvent.click(screen.getByRole('button',{name:'登录'}));await waitFor(()=>expect(screen.getByText('RoleLoom')).toBeTruthy());expect(fetchMock).toHaveBeenCalledWith('/api/auth/login',expect.objectContaining({method:'POST'}))
  })
})
