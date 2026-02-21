import { useState } from 'react';
import { 
  Search, Github, Plus, Copy, ChevronLeft, ChevronRight, LayoutGrid 
} from 'lucide-react';

type NavItem =
  | 'Introduction'
  | 'Components'
  | 'Installation'
  | 'Directory'
  | 'RTL'
  | 'MCP Server';

const App = () => {
  const [selectedItem, setSelectedItem] = useState<NavItem>('Installation');
  const [pkgManager, setPkgManager] = useState<'pnpm' | 'npm' | 'yarn' | 'bun'>('bun');

  return (
    <div className="sans min-h-screen bg-white text-slate-950 font-sans antialiased">
      
      {/* TOP NAV */}
      <header className="sticky top-0 z-50 w-full bg-white/95 backdrop-blur supports-[backdrop-filter]:bg-white/60">
        <div className="container mx-auto flex h-14 items-center justify-between px-8">

          {/* LEFT */}
          <div className="mr-4 flex items-center gap-8">
            <a href="/" className="flex items-center space-x-2 font-bold">
              <img src="/log/lilDockerr.png" className="w-10" />
              <span>lilDocker</span>
            </a>

            <nav className="flex items-center space-x-6 text-sm font-medium text-slate-600">
              {['Docs', 'Components', 'Blocks', 'Charts', 'Directory', 'Create'].map(
                (link) => (
                  <a
                    key={link}
                    href="#"
                    className="transition-colors hover:text-slate-950"
                  >
                    {link}
                  </a>
                )
              )}
            </nav>
          </div>

          {/* RIGHT */}
          <div className="flex flex-1 items-center justify-end space-x-4">
            <div className="relative w-full max-w-[260px]">
              <Search className="absolute left-2 top-2.5 h-4 w-4 text-slate-400" />
              <input
                type="text"
                placeholder="Search documentation..."
                className="h-9 w-full rounded-md border border-slate-200 bg-slate-50 pl-8 pr-4 text-sm focus:outline-none focus:ring-1 focus:ring-slate-400"
              />
            </div>

            <div className="flex items-center gap-2 border-l pl-4 border-slate-200">
              <div className="flex items-center gap-1 text-sm font-medium">
                <Github className="h-4 w-4" /> 107k
              </div>

              <button className="flex items-center gap-1 rounded-md bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800 transition-colors">
                <Plus className="h-4 w-4" /> New Project
              </button>
            </div>
          </div>
        </div>
      </header>

      {/* BODY */}
      <div className="container mx-auto flex px-8">

        {/* LEFT SIDEBAR */}
        <aside className="fixed top-14 z-30 -ml-2 hidden h-[calc(100vh-3.5rem)] w-full shrink-0 overflow-y-auto md:sticky md:block md:w-64 py-8 pr-6">
          <div className="space-y-6">

            <div>
              <h4 className="mb-2 px-2 text-sm font-semibold tracking-tight text-slate-900">
                Sections
              </h4>

              <div className="grid text-sm gap-0.5">
                {(
                  [
                    'Introduction',
                    'Components',
                    'Installation',
                    'Directory',
                    'RTL',
                    'MCP Server',
                  ] as NavItem[]
                ).map((item) => (
                  <button
                    key={item}
                    onClick={() => setSelectedItem(item)}
                    className={`flex w-full items-center rounded-md border border-transparent px-2 py-1.5 underline-offset-4 ${
                      selectedItem === item
                        ? 'font-medium text-slate-900 bg-slate-100'
                        : 'text-slate-500 hover:text-slate-950'
                    }`}
                  >
                    {item}
                    {item === 'RTL' && (
                      <span className="ml-2 h-1.5 w-1.5 rounded-full bg-blue-600" />
                    )}
                  </button>
                ))}
              </div>
            </div>

            <div>
              <h4 className="mb-2 px-2 text-sm font-semibold tracking-tight text-slate-900">
                Components
              </h4>

              <div className="grid text-sm gap-0.5 text-slate-500">
                <a href="#" className="px-2 py-1.5 hover:underline">
                  Accordion
                </a>
                <a href="#" className="px-2 py-1.5 hover:underline">
                  Alert
                </a>
              </div>
            </div>

          </div>
        </aside>

        {/* MAIN */}
        <main className="relative py-8 lg:gap-10 lg:py-10 flex-1 px-4 lg:px-10">

          {selectedItem === 'Installation' ? (
            <div className="mx-auto max-w-3xl space-y-6">

              {/* HEADER */}
              <div className="flex items-center justify-between">
                <div className="space-y-2">
                  <h1 className="text-4xl font-bold">Installation</h1>
                  <p className="text-lg text-slate-500">
                    How to install dependencies and structure your app.
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  <button className="inline-flex items-center gap-1.5 rounded-md border border-slate-200 px-3 py-1.5 text-sm font-medium hover:bg-slate-50">
                    <Copy className="h-3.5 w-3.5" />
                    Copy Page
                  </button>

                  <div className="flex border rounded-md overflow-hidden">
                    <button className="p-1.5 hover:bg-slate-50 border-r">
                      <ChevronLeft className="h-4 w-4" />
                    </button>
                    <button className="p-1.5 hover:bg-slate-50">
                      <ChevronRight className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              </div>

              {/* NOTICE */}
              <div className="rounded-lg border border-green-200 bg-green-50 p-4 text-[15px] text-green-900">
                Starting a new project? Use{" "}
                <span className="font-mono font-bold underline cursor-pointer">
                  shadcn/create
                </span>{" "}
                to scaffold a complete app.
              </div>

              {/* QUICK START */}
              <div className="space-y-4">
                <h2 className="text-2xl font-semibold">Quick Start</h2>
                <p className="text-slate-600">
                  Run the following command:
                </p>

                <div className="rounded-lg border border-slate-200 overflow-hidden">
                  
                  {/* TABS */}
                  <div className="flex border-b bg-slate-50 px-4">
                    {(['pnpm', 'npm', 'yarn', 'bun'] as const).map((mgr) => (
                      <button
                        key={mgr}
                        onClick={() => setPkgManager(mgr)}
                        className={`px-4 py-2 text-sm ${
                          pkgManager === mgr
                            ? 'text-slate-950 font-medium'
                            : 'text-slate-500'
                        }`}
                      >
                        {mgr}
                      </button>
                    ))}
                  </div>

                  {/* COMMAND */}
                  <div className="p-6 font-mono text-sm">
                    {pkgManager === 'bun'
                      ? 'bunx --bun shadcn@latest create'
                      : `${pkgManager} dlx shadcn@latest create`}
                  </div>
                </div>
              </div>

            </div>
          ) : (
            <div className="flex flex-col items-center justify-center py-20 text-slate-400">
              <LayoutGrid className="h-12 w-12 mb-4 opacity-20" />
              <p className="text-xl font-medium text-slate-900">
                This is the {selectedItem} page
              </p>
            </div>
          )}

        </main>

      </div>
    </div>
  );
};

export default App;