import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import ts from 'typescript'
import * as addresses from '../src/addresses.ts'
import * as navigation from '../src/navigation.ts'

// Execute the actual setup script with controlled navigation/network, without a server or secrets.
function consoleHarness(fetcher){
 const route={fullPath:'/login',name:'login',meta:{},params:{}}
 const router={replace:async target=>{route.name=target.name||'subscriptions';route.fullPath=typeof target==='string'?target:'/subscriptions'},push:async()=>{}}
 const vue={ref:value=>({value}),reactive:value=>value,computed:fn=>({get value(){return fn()}}),watch:()=>{},onMounted:()=>{},nextTick:async()=>{}}
 const modules={vue,'vue-router':{useRoute:()=>route,useRouter:()=>router},'./addresses':addresses,'./navigation':navigation}
 const source=readFileSync(new URL('../src/HubConsole.vue',import.meta.url),'utf8').match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
 const code=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
 const view=new Function('require','exports','window','fetch','confirm',code+'\nreturn {starting,authenticated,csrf,busy,error,signIn,api,action,syncRoute,changePassword,password,editSubscription,subscriptionDraft,entities};')(
  name=>modules[name],{}, {location:{href:'https://fixture.example/custom/#/login'}},fetcher,()=>true)
 view.starting.value=false
 return {view,route}
}
const response=value=>({ok:true,status:200,json:async()=>value})
const deferred=()=>{let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve}}

test('login initialization survives a route change while its lists are loading',async()=>{
 const data=deferred(),started=deferred()
 const {view,route}=consoleHarness(async url=>{
  if(url.endsWith('/login'))return response({username:'fixture',csrf:'fixture-csrf'})
  started.resolve();await data.promise;return response([])
 })
 const login=view.signIn();await started.promise
 Object.assign(route,{name:'server',fullPath:'/servers',meta:{tab:'server'}})
 await view.syncRoute();data.resolve();await login
 assert.equal(view.authenticated.value,true)
 assert.equal(view.error.value,'')
 assert.equal(route.fullPath,'/servers')
})

test('logout and password changes commit their session state after navigation',async()=>{
 for(const operation of ['logout','password']){
  const result=deferred(),started=deferred()
  const {view,route}=consoleHarness(async()=>{started.resolve();await result.promise;return response({ok:true})})
  view.authenticated.value=true;view.csrf.value='fixture-csrf'
  const pending=operation==='logout'?view.action(async()=>{await view.api('/logout','POST',{});view.authenticated.value=false;view.csrf.value=''}):view.changePassword()
  await started.promise
  Object.assign(route,{name:'server',fullPath:'/servers',meta:{tab:'server'}})
  await view.syncRoute();result.resolve();await pending
  assert.equal(view.authenticated.value,false,operation)
  assert.equal(view.csrf.value,'',operation)
 }
})

test('subscriptions can explicitly select direct mode while editing preserves an existing proxy',()=>{
 const {view}=consoleHarness(async()=>response([]))
 view.entities.proxy.push({id:'fixture-proxy',label:'Proxy'})
 view.editSubscription()
 assert.equal(view.subscriptionDraft.value.proxyId,'')
 view.editSubscription({id:'fixture-sub',proxyId:'fixture-proxy'})
 assert.equal(view.subscriptionDraft.value.proxyId,'fixture-proxy')
 const source=readFileSync(new URL('../src/HubConsole.vue',import.meta.url),'utf8')
 assert.match(source,/<select v-model="subscriptionDraft\.proxyId"><option value="">不使用代理（直连）<\/option>/)
})