// web/src/router-additions.tsx
//
// EvolvX v1.1 router additions.
//
// ─── HOW TO INTEGRATE ────────────────────────────────────────────────────────
//
// 1. In your existing router file (typically web/src/App.tsx or web/src/router.tsx),
//    add the three new route imports and route definitions shown below.
//
// 2. In your sidebar / nav component, add the three nav items shown below.
//
// ─── STEP 1: Add to your router ──────────────────────────────────────────────
//
// import Registry  from './pages/Registry'
// import Journal   from './pages/Journal'
// import Optimizer from './pages/Optimizer'
//
// // Inside your <Routes> or router config:
// <Route path="/registry"  element={<Registry />} />
// <Route path="/journal"   element={<Journal />} />
// <Route path="/optimizer" element={<Optimizer />} />
//
// ─── STEP 2: Add to your sidebar nav ─────────────────────────────────────────
//
// The three new nav items to add alongside your existing Dashboard / Strategy
// Studio / Live Trading / Backtest links:

export const EVOLVX_NAV_ITEMS = [
  {
    path: '/registry',
    label: 'Registry',
    icon: '🗂',
    description: 'Strategy version history & lineage',
    badge: 'NEW',
  },
  {
    path: '/journal',
    label: 'Journal',
    icon: '📓',
    description: 'Decision memory & outcome analysis',
    badge: 'NEW',
  },
  {
    path: '/optimizer',
    label: 'Optimizer',
    icon: '⚙️',
    description: 'Walk-forward strategy evolution',
    badge: 'NEW',
  },
] as const

// ─── STEP 3: Example nav item component ──────────────────────────────────────
//
// If your nav uses a component like <NavLink>, here's how to render the new items:
//
// import { NavLink } from 'react-router-dom'
// import { EVOLVX_NAV_ITEMS } from './router-additions'
//
// function EvolvXNavSection() {
//   return (
//     <div className="mt-4 pt-4 border-t border-zinc-800">
//       <p className="px-3 mb-2 text-[9px] font-mono text-zinc-600 uppercase tracking-widest">
//         EvolvX v1.1
//       </p>
//       {EVOLVX_NAV_ITEMS.map(item => (
//         <NavLink
//           key={item.path}
//           to={item.path}
//           className={({ isActive }) =>
//             `flex items-center gap-3 px-3 py-2 rounded-lg text-xs font-mono transition-colors ${
//               isActive
//                 ? 'bg-amber-400/10 text-amber-400 border border-amber-400/20'
//                 : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800'
//             }`
//           }
//         >
//           <span className="text-base">{item.icon}</span>
//           <span>{item.label}</span>
//           {item.badge && (
//             <span className="ml-auto text-[9px] px-1.5 py-0.5 rounded bg-amber-400 text-zinc-950 font-bold">
//               {item.badge}
//             </span>
//           )}
//         </NavLink>
//       ))}
//     </div>
//   )
// }
//
// ─── STEP 4: No backend changes needed ───────────────────────────────────────
//
// The three pages connect exclusively to:
//   GET/POST /api/v1/registry/*    (from api/registry_handlers.go)
//   GET/POST /api/v1/journal/*     (from api/registry_handlers.go)
//   GET/POST /api/v1/optimizer/*   (from api/registry_handlers.go)
//
// All three handler groups are registered via api.RegisterRegistryRoutes(),
// api.RegisterJournalRoutes(), and api.RegisterOptimizerRoutes() in api/services.go.
// These are mounted in your main.go or server.go as described in docs/MIGRATION.md.
//
// The pages also call the existing /api/traders endpoint to populate strategy
// selectors — no change needed there.

export default EVOLVX_NAV_ITEMS
