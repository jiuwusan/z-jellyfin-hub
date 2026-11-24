import test from 'node:test'
import assert from 'node:assert/strict'
import { deploymentBase, subscriptionLink } from '../src/addresses.ts'

test('current browser address determines links without a host allowlist', () => {
 const path='api/v1/subscriptions/0123456789abcdef0123456789abcdef/config#'+'A'.repeat(43)
 for(const href of ['https://hub.example/','http://192.0.2.5:8080/','https://[2001:db8::5]:9443/hub/','https://other.example/hub/index.html?x=1#old']) {
  const base=deploymentBase(href)
  assert.equal(base.search,'');assert.equal(base.hash,'')
  assert.equal(subscriptionLink(path,href),new URL(path,base).href)
 }
 assert.equal(deploymentBase('https://new.example/hub').pathname,'/hub/')
 assert.throws(()=>subscriptionLink('https://evil.example/config','https://hub.example/'))
})
