import test from 'node:test'
import assert from 'node:assert/strict'
import { deploymentBase, subscriptionLink, normalizeToken, normalizeRestoredToken } from '../src/addresses.ts'

test('current browser address determines links without a host allowlist', () => {
 const path='api/v1/subscription/config#'+'A'.repeat(43)
 for(const href of ['https://hub.example/','http://192.0.2.5:8080/','https://[2001:db8::5]:9443/hub/','https://other.example/hub/index.html?x=1#old']) {
  const base=deploymentBase(href)
  assert.equal(base.search,'');assert.equal(base.hash,'')
  assert.equal(subscriptionLink(path,href),new URL(path,base).href)
 }
 assert.equal(deploymentBase('https://new.example/hub').pathname,'/hub/')
 assert.throws(()=>subscriptionLink('https://evil.example/config','https://hub.example/'))
})

test('manual tokens are canonical and can be restored without exposing an ID',()=>{assert.equal(normalizeToken('A'.repeat(43)),'A'.repeat(43));assert.equal(normalizeToken(''),'');assert.throws(()=>normalizeToken('short'));assert.throws(()=>normalizeToken('A'.repeat(42)+'B'));assert.throws(()=>subscriptionLink('api/v1/subscriptions/0123456789abcdef0123456789abcdef/config#'+'A'.repeat(43),'https://hub.example/'))})

test('legacy restoration accepts only the original token or a complete subscription link',()=>{
 const token='A'.repeat(43)
 assert.equal(normalizeRestoredToken(' '+token+' '),token)
 for(const prefix of ['https://hub.example/','http://192.0.2.4:8097/prefix/'])
  assert.equal(normalizeRestoredToken(prefix+'api/v1/subscription/config#'+token),token)
 for(const value of ['', 'https://hub.example/#/subscriptions/'+token, 'https://hub.example/api/v1/subscription/config?token=x#'+token,
   'https://user:password@hub.example/api/v1/subscription/config#'+token, 'javascript:alert(1)', 'https://hub.example/api/v1/subscription/config#bad'])
  assert.throws(()=>normalizeRestoredToken(value))
})
