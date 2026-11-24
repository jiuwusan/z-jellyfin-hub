import test from 'node:test'
import assert from 'node:assert/strict'
import {RequestScope,StaleNavigation,runRequest} from '../src/navigation.ts'

test('late responses cannot reopen a secret dialog after navigation',async()=>{
 const scope=new RequestScope();let release
 const deferred=new Promise(resolve=>release=resolve)
 const result=scope.run(()=>deferred)
 scope.invalidate();release({link:'fixture-secret'})
 await assert.rejects(result,StaleNavigation)
 assert.equal(await scope.run(async()=>42),42)
})

test('errors from a previous route are ignored too',async()=>{
 const scope=new RequestScope();let reject
 const result=scope.run(()=>new Promise((_,fail)=>reject=fail))
 scope.invalidate();reject(new Error('fixture failure'))
 await assert.rejects(result,StaleNavigation)
})

test('session operations still complete after a route changes',async()=>{
 for(const path of ['/login','/logout','/password','/session']){
  const scope=new RequestScope();let release
  const result=runRequest(path,scope,()=>new Promise(resolve=>release=resolve))
  scope.invalidate();release('session-result')
  assert.equal(await result,'session-result')
 }
})
