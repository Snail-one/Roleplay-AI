export class ApiError extends Error {
  constructor(message: string, public readonly status: number) { super(message); this.name = 'ApiError' }
}

export interface ModelProfile { id:string; name:string; provider:string; api_url:string; has_api_key:boolean; model:string; timeout_seconds:number; max_output_tokens:number; context_window:number; is_default:boolean }
export interface Character { id:string; name:string; bio:string; personality:string; scenario:string; greeting:string; system_rules:string; example_dialogue:string; enable_time:boolean; enable_calculator:boolean; default_model_profile_id:string|null; has_avatar:boolean }
export interface CharacterSnapshot { name:string; bio:string; personality:string; scenario:string; greeting:string; system_rules:string; example_dialogue:string; enable_time:boolean; enable_calculator:boolean }
export interface Conversation { id:string; title:string; character_id:string; character_snapshot:CharacterSnapshot; model_profile_id:string; created_at:string; updated_at:string }
export interface Message { id:string; conversation_id:string; seq:number; role:'user'|'assistant'|'tool'; content:string; status:'generating'|'completed'|'failed'|'cancelled'; client_message_id?:string; created_at:string; updated_at:string }
type ModelSave = Partial<ModelProfile>&{api_key?:string;clear_api_key?:boolean}
type CharacterSave = Partial<Character>

interface ErrorPayload { error?: { message?: string } }

async function errorMessage(response:Response) {
  try { const body = await response.json() as ErrorPayload; return body.error?.message || `请求失败（${response.status}）` } catch { return `请求失败（${response.status}）` }
}

export async function request<T>(path:string, init:RequestInit = {}):Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type','application/json')
  const response = await fetch(path,{...init,headers,credentials:'same-origin'})
  if (!response.ok) throw new ApiError(await errorMessage(response),response.status)
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

async function uploadAvatar(id:string,file:File){const response=await fetch(`/api/characters/${id}/avatar`,{method:'PUT',credentials:'same-origin',headers:{'Content-Type':file.type},body:file});if(!response.ok)throw new ApiError(await errorMessage(response),response.status)}

export const api = {
  session: () => request<{authenticated:boolean}>('/api/auth/session'),
  login: (password:string) => request('/api/auth/login',{method:'POST',body:JSON.stringify({password})}),
  logout: () => request('/api/auth/logout',{method:'POST',body:'{}'}),
  models: () => request<ModelProfile[]>('/api/model-profiles'),
  saveModel: (value:object) => { const v=value as ModelSave,payload={name:v.name,provider:v.provider,api_url:v.api_url,api_key:v.api_key,clear_api_key:v.clear_api_key,model:v.model,timeout_seconds:v.timeout_seconds,max_output_tokens:v.max_output_tokens,context_window:v.context_window,is_default:v.is_default};return v.id?request<ModelProfile>(`/api/model-profiles/${v.id}`,{method:'PATCH',body:JSON.stringify(payload)}):request<ModelProfile>('/api/model-profiles',{method:'POST',body:JSON.stringify(payload)}) },
  deleteModel: (id:string) => request<void>(`/api/model-profiles/${id}`,{method:'DELETE',body:'{}'}),
  testModel: (id:string) => request<{success:boolean;message:string;category?:string}>(`/api/model-profiles/${id}/test`,{method:'POST',body:'{}'}),
  characters: () => request<Character[]>('/api/characters'),
  saveCharacter: (value:object) => { const v=value as CharacterSave,payload={name:v.name,bio:v.bio,personality:v.personality,scenario:v.scenario,greeting:v.greeting,system_rules:v.system_rules,example_dialogue:v.example_dialogue,enable_time:v.enable_time,enable_calculator:v.enable_calculator,default_model_profile_id:v.default_model_profile_id};return v.id?request<Character>(`/api/characters/${v.id}`,{method:'PATCH',body:JSON.stringify(payload)}):request<Character>('/api/characters',{method:'POST',body:JSON.stringify(payload)}) },
  deleteCharacter: (id:string) => request<void>(`/api/characters/${id}`,{method:'DELETE',body:'{}'}),
  uploadAvatar,
  deleteAvatar: (id:string) => request<void>(`/api/characters/${id}/avatar`,{method:'DELETE',body:'{}'}),
  conversations: () => request<Conversation[]>('/api/conversations'),
  createConversation: (character_id:string,model_profile_id?:string) => request<Conversation>('/api/conversations',{method:'POST',body:JSON.stringify({character_id,model_profile_id:model_profile_id||undefined})}),
  deleteConversation: (id:string) => request<void>(`/api/conversations/${id}`,{method:'DELETE',body:'{}'}),
  messages: (id:string) => request<Message[]>(`/api/conversations/${id}/messages?limit=200`),
  editMessage: (conversationID:string,messageID:string,content:string) => request<Message>(`/api/conversations/${conversationID}/messages/${messageID}`,{method:'PATCH',body:JSON.stringify({content})}),
  deleteMessage: (conversationID:string,messageID:string) => request<void>(`/api/conversations/${conversationID}/messages/${messageID}`,{method:'DELETE',body:'{}'}),
  stop: (id:string) => request(`/api/conversations/${id}/stop`,{method:'POST',body:'{}'}),
}

export async function streamEvents(path:string, body:unknown, onEvent:(type:string,data:unknown)=>void, signal?:AbortSignal) {
  const response = await fetch(path,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','Accept':'text/event-stream'},body:JSON.stringify(body),signal})
  if (!response.ok) throw new ApiError(await errorMessage(response),response.status)
  if (!response.body) throw new ApiError('浏览器不支持流式响应',500)
  const reader=response.body.getReader(), decoder=new TextDecoder(); let buffer=''
  for (;;) {
    const {done,value}=await reader.read(); buffer+=decoder.decode(value,{stream:!done}).replace(/\r\n/g,'\n')
    let boundary:number
    while ((boundary=buffer.indexOf('\n\n'))>=0) {
      const block=buffer.slice(0,boundary);buffer=buffer.slice(boundary+2);let type='message',data=''
      for(const line of block.split('\n')){if(line.startsWith('event:'))type=line.slice(6).trim();if(line.startsWith('data:'))data+=line.slice(5).trim()}
      if(data){try{onEvent(type,JSON.parse(data))}catch{onEvent('error',{message:'服务器返回了无效的流事件'})}}
    }
    if(done)break
  }
}
