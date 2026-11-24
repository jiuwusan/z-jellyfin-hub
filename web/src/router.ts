import {createRouter,createWebHashHistory} from 'vue-router'
import type {RouteComponent,RouteRecordRaw,RouterHistory} from 'vue-router'

const consolePage=()=>import('./HubConsole.vue')

export function createRoutes(component:RouteComponent=consolePage):RouteRecordRaw[]{
 return [
  {path:'/',redirect:{name:'subscriptions',params:{}}},
  {path:'/setup',name:'setup',component},
  {path:'/login',name:'login',component},
  {path:'/subscriptions',name:'subscriptions',component,meta:{tab:'subscriptions'}},
  {path:'/subscriptions/:id([0-9a-f]{32})',name:'subscription-detail',component,meta:{tab:'subscriptions'}},
  ...([['proxy','/proxies'],['server','/servers'],['credential','/credentials'],['policy','/policies']] as const)
   .map(([name,path])=>({path,name,component,meta:{tab:name}})),
  {path:'/:pathMatch(.*)*',redirect:{name:'subscriptions',params:{}}}
 ]
}

export function createHubRouter(history:RouterHistory=createWebHashHistory()){
 return createRouter({history,routes:createRoutes()})
}
