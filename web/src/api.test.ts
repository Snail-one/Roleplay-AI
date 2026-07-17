import { describe, expect, it, vi } from 'vitest'
import { streamEvents } from './api'

describe('streamEvents',()=>{
  it('parses SSE events split across network chunks',async()=>{
    const encoder=new TextEncoder();const chunks=['event: assistant_delta\ndata: {"delta":"你','好"}\n\nevent: assistant_done\ndata: {"id":"m1"}\n\n'];let index=0
    const body=new ReadableStream<Uint8Array>({pull(controller){if(index===chunks.length){controller.close();return}controller.enqueue(encoder.encode(chunks[index++]))}})
    vi.stubGlobal('fetch',vi.fn(async()=>new Response(body,{status:200,headers:{'Content-Type':'text/event-stream'}})))
    const events:Array<[string,unknown]>=[];await streamEvents('/stream',{},(type,data)=>events.push([type,data]));expect(events).toEqual([['assistant_delta',{delta:'你好'}],['assistant_done',{id:'m1'}]])
  })
})
