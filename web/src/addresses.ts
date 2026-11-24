export function deploymentBase(href: string): URL {
 const base=new URL(href)
 base.search='';base.hash=''
 if(base.pathname.endsWith('/index.html')) base.pathname=base.pathname.slice(0,-10)
 if(!base.pathname.endsWith('/')) base.pathname+='/'
 return base
}

export function subscriptionLink(path: string, href: string): string {
 if(!/^api\/v1\/subscription\/config#[A-Za-z0-9_-]{43}$/.test(path))
  throw new Error('订阅链接格式无效，请刷新后台后重试')
 return new URL(path,deploymentBase(href)).href
}

export function normalizeToken(input: string): string {
 const token=input.trim()
 if(!token)return ''
 if(!/^[A-Za-z0-9_-]{43}$/.test(token))throw new Error('请输入原来的 43 位订阅令牌')
 const raw=atob(token.replaceAll('-','+').replaceAll('_','/')+'=')
 if(raw.length!==32||btoa(raw).replaceAll('+','-').replaceAll('/','_').replace(/=+$/,'')!==token)
  throw new Error('订阅令牌格式无效')
 return token
}

export function normalizeRestoredToken(input: string): string {
 const value=input.trim()
 if(!value)throw new Error('请输入原订阅链接或令牌')
 if(!value.includes('://'))return normalizeToken(value)
 let url:URL
 try{url=new URL(value)}catch{throw new Error('原订阅链接格式无效')}
 if(!['https:','http:'].includes(url.protocol)||url.username||url.password||url.search
    ||!url.pathname.endsWith('/api/v1/subscription/config')||!url.hash)
  throw new Error('请输入完整的原订阅链接，或只填写原令牌')
 return normalizeToken(url.hash.slice(1))
}
