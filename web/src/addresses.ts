export function deploymentBase(href: string): URL {
 const base=new URL(href)
 base.search='';base.hash=''
 if(base.pathname.endsWith('/index.html')) base.pathname=base.pathname.slice(0,-10)
 if(!base.pathname.endsWith('/')) base.pathname+='/'
 return base
}

export function subscriptionLink(path: string, href: string): string {
 if(!/^api\/v1\/subscriptions\/[0-9a-f]{32}\/config#[A-Za-z0-9_-]{43}$/.test(path))
  throw new Error('订阅链接格式无效，请刷新后台后重试')
 return new URL(path,deploymentBase(href)).href
}
