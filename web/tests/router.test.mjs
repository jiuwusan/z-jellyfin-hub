import test from 'node:test'
import assert from 'node:assert/strict'
import {createRouter,createMemoryHistory} from 'vue-router'
import {createRoutes} from '../src/router.ts'
import {deploymentBase,subscriptionLink} from '../src/addresses.ts'

test('management routes support direct detail URLs and history navigation',async()=>{
 const router=createRouter({history:createMemoryHistory(),routes:createRoutes({render:()=>null})})
 const id='0123456789abcdef0123456789abcdef'
 await router.push('/subscriptions/'+id)
 assert.equal(router.currentRoute.value.name,'subscription-detail')
 assert.equal(router.currentRoute.value.params.id,id)
 await router.push('/policies')
 assert.equal(router.currentRoute.value.meta.tab,'policy')
 const back=new Promise(resolve=>{const off=router.afterEach(()=>{off();resolve()})})
 router.back();await back
 assert.equal(router.currentRoute.value.params.id,id)
 await router.push('/anything-unknown');assert.equal(router.currentRoute.value.name,'subscriptions')
})

test('hash routes never become resource/API prefixes or subscription tokens',()=>{
 for(const prefix of ['/','/custom-console/','/tools/video/control/','/api/v1/portal/']){
  const href='https://proxy.example:9443'+prefix+'#/subscriptions/0123456789abcdef0123456789abcdef?tab=history'
  assert.equal(new URL('api/v1/',deploymentBase(href)).href,'https://proxy.example:9443'+prefix+'api/v1/')
  const link=subscriptionLink('api/v1/subscription/config#'+'A'.repeat(43),href)
  assert.equal(new URL(link).pathname,prefix+'api/v1/subscription/config')
  assert.equal(new URL(link).hash,'#'+'A'.repeat(43))
 }
})
