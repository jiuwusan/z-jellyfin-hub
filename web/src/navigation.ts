export class StaleNavigation extends Error {}

// Authentication changes outlive the page on which they were requested.
export function runRequest<T>(path:string,scope:RequestScope,work:()=>Promise<T>):Promise<T>{
 return ['/login','/logout','/password','/session'].includes(path)?work():scope.run(work)
}

export class RequestScope {
 private epoch=0
 invalidate(){this.epoch++}
 async run<T>(work:()=>Promise<T>):Promise<T>{
  const expected=this.epoch
  try{
   const result=await work()
   if(expected!==this.epoch)throw new StaleNavigation()
   return result
  }catch(error){
   if(expected!==this.epoch)throw new StaleNavigation()
   throw error
  }
 }
}
