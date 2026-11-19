<template>
  <div class="space-y-4 mb-5">
    <!-- Active Circuit Alert Banner (if circuit is open) -->
    <div
      v-if="isCircuitActive"
      class="rounded-2xl border border-destructive/30 bg-destructive/10 p-4 text-foreground shadow-xs animate-in fade-in"
    >
      <div class="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
        <div class="flex items-start gap-3">
          <div class="p-2 rounded-full bg-destructive/20 text-destructive mt-0.5 sm:mt-0">
            <Flame class="size-5 animate-pulse" />
          </div>
          <div>
            <div class="flex items-center gap-2">
              <h3 class="text-sm font-semibold text-destructive flex items-center gap-1.5">
                {{ t("dashboard.circuit_active_banner") }}
              </h3>
              <Badge variant="destructive" class="text-[10px] font-mono px-2 py-0 h-4.5">
                {{ activeReasonLabel }}
              </Badge>
            </div>
            <p class="text-xs text-muted-foreground mt-1">
              {{ t("dashboard.circuit_active_desc") }}
              <span v-if="cooldownLeft > 0" class="font-mono font-medium text-foreground ml-1">
                ({{ t("dashboard.cooldown_remaining") }}: {{ Math.ceil(cooldownLeft) }}s)
              </span>
            </p>
          </div>
        </div>
        <div class="flex items-center gap-2 self-end sm:self-center">
          <Button
            v-if="activeIncidentId"
            variant="outline"
            size="xs"
            class="h-8 gap-1.5 border-destructive/30 hover:bg-destructive/10 hover:text-destructive"
            @click="openIncident(activeIncidentId)"
          >
            <Eye class="size-3.5" />
            {{ t("dashboard.view_postmortem") }}
          </Button>
          <Button
            variant="destructive"
            size="xs"
            class="h-8 gap-1.5"
            :disabled="s.resettingCircuit.value"
            @click="confirmReset"
          >
            <RotateCcw class="size-3.5" :class="{ 'animate-spin': s.resettingCircuit.value }" />
            {{ t("dashboard.reset_circuit_btn") }}
          </Button>
        </div>
      </div>
    </div>

    <!-- Time Range Selector & Top Bar -->
    <div class="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2.5 pt-1">
      <div class="inline-flex items-center gap-1 p-1 rounded-full bg-muted/50 border border-border/60 text-xs font-mono">
        <button
          type="button"
          class="px-3 py-1 rounded-full transition-all text-xs font-medium cursor-pointer"
          :class="
            currentRange === '24h'
              ? 'bg-background text-foreground shadow-xs font-semibold'
              : 'text-muted-foreground hover:text-foreground'
          "
          @click="selectRange('24h')"
        >
          {{ t("dashboard.range_24h") }}
        </button>
        <button
          type="button"
          class="px-3 py-1 rounded-full transition-all text-xs font-medium cursor-pointer"
          :class="
            currentRange === '7d'
              ? 'bg-background text-foreground shadow-xs font-semibold'
              : 'text-muted-foreground hover:text-foreground'
          "
          @click="selectRange('7d')"
        >
          {{ t("dashboard.range_7d") }}
        </button>
      </div>

      <div class="flex items-center gap-3 text-xs text-muted-foreground font-mono">
        <span class="hidden sm:inline">
          {{ t("dashboard.updated_at") }}: {{ lastUpdateTime }}
        </span>
        <Button
          variant="ghost"
          size="xs"
          class="h-7 text-xs gap-1 text-muted-foreground hover:text-foreground"
          @click="refreshData"
        >
          <RotateCcw class="size-3" />
          <span>{{ t("common.refresh") || "刷新" }}</span>
        </Button>
      </div>
    </div>

    <!-- 4 Big Number KPI Cards (Canonical Shadcn Luma Pattern) -->
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-3.5">
      <Card class="shadow-xs hover:shadow-sm transition-shadow">
        <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
          <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t("dashboard.period_requests") || t("dashboard.outbound_requests") }}
          </CardTitle>
          <CardAction>
            <Activity class="size-4 text-muted-foreground" />
          </CardAction>
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold tracking-tight font-mono text-foreground">
            {{ summary.total_requests }}
          </div>
        </CardContent>
      </Card>

      <Card class="shadow-xs hover:shadow-sm transition-shadow">
        <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
          <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t("dashboard.success_rate") }}
          </CardTitle>
          <CardAction>
            <CheckCircle2 class="size-4 text-muted-foreground" />
          </CardAction>
        </CardHeader>
        <CardContent>
          <div
            class="text-2xl font-bold tracking-tight font-mono"
            :class="summary.success_rate < 90 ? 'text-destructive' : 'text-foreground'"
          >
            {{ summary.success_rate }}%
          </div>
        </CardContent>
      </Card>

      <Card class="shadow-xs hover:shadow-sm transition-shadow">
        <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
          <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t("dashboard.failures_count") }}
          </CardTitle>
          <CardAction>
            <AlertCircle class="size-4 text-muted-foreground" />
          </CardAction>
        </CardHeader>
        <CardContent>
          <div
            class="text-2xl font-bold tracking-tight font-mono"
            :class="summary.failures > 0 ? 'text-destructive' : 'text-foreground'"
          >
            {{ summary.failures }}
          </div>
        </CardContent>
      </Card>

      <Card class="shadow-xs hover:shadow-sm transition-shadow">
        <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
          <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t("dashboard.avg_duration") }}
          </CardTitle>
          <CardAction>
            <Clock class="size-4 text-muted-foreground" />
          </CardAction>
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold tracking-tight font-mono text-foreground">
            {{ summary.avg_duration_ms }} <span class="text-xs font-normal text-muted-foreground">ms</span>
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Main Visual: Request Activity Bar Chart (Luma Taupe Theme) -->
    <Card class="shadow-xs hover:shadow-sm transition-shadow">
      <CardHeader class="pb-3 border-b border-border/50">
        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <div>
            <CardTitle class="text-base font-semibold text-foreground">
              {{ t("dashboard.request_activity") }}
            </CardTitle>
            <CardDescription class="text-xs text-muted-foreground mt-0.5">
              {{ t("dashboard.request_activity_desc") }}
            </CardDescription>
          </div>

          <!-- Legend -->
          <div class="flex items-center gap-3.5 text-xs text-muted-foreground font-mono">
            <span class="flex items-center gap-1.5">
              <span class="size-2 rounded-full bg-emerald-500"></span>
              {{ t("dashboard.success_req") }}
            </span>
            <span class="flex items-center gap-1.5">
              <span class="size-2 rounded-full bg-amber-500"></span>
              {{ t("dashboard.error_req") }}
            </span>
            <span class="flex items-center gap-1.5">
              <span class="size-2 rounded-full bg-destructive"></span>
              {{ t("dashboard.circuit_req") }}
            </span>
          </div>
        </div>
      </CardHeader>

      <CardContent class="pt-5 pb-4 space-y-3">
        <!-- The Bar Chart Container -->
        <div class="relative h-44 sm:h-52 w-full flex flex-col justify-end">
          <!-- Background Y-Axis Guide Lines -->
          <div class="absolute inset-x-0 inset-y-0 flex flex-col justify-between pointer-events-none">
            <div class="border-b border-border/40 w-full flex items-center justify-end pr-1 text-[10px] font-mono text-muted-foreground/60">
              {{ maxBarVolume }}
            </div>
            <div class="border-b border-border/25 w-full flex items-center justify-end pr-1 text-[10px] font-mono text-muted-foreground/50">
              {{ Math.round(maxBarVolume / 2) }}
            </div>
            <div class="border-b border-border/40 w-full flex items-center justify-end pr-1 text-[10px] font-mono text-muted-foreground/60">
              0
            </div>
          </div>

          <!-- Bars Flow -->
          <TooltipProvider :delay-duration="50">
            <div
              class="relative z-10 w-full h-full flex items-end px-1"
              :class="currentRange === '7d' ? 'gap-0.5 sm:gap-1' : 'gap-1 sm:gap-1.5'"
            >
              <Tooltip v-for="(slot, idx) in activitySlots" :key="slot.timestamp || idx">
                <TooltipTrigger as-child>
                  <div
                    class="flex-1 h-full flex flex-col justify-end items-center group cursor-pointer"
                  >
                    <!-- Bar Column -->
                    <div
                      class="w-full rounded-t-xs transition-all duration-200 flex flex-col justify-end overflow-hidden"
                      :style="{
                        height: barHeightPercent(slot),
                      }"
                    >
                      <!-- Circuit Open or Blocked slice (top, red) -->
                      <div
                        v-if="slot.status === 'circuit_open' || slot.blocked_count > 0"
                        class="w-full bg-destructive transition-all"
                        :class="{ 'animate-pulse': slot.status === 'circuit_open' }"
                        :style="{ height: blockedHeightPercent(slot) }"
                      ></div>
                      <!-- Error / Failure slice (middle, amber) -->
                      <div
                        v-if="slot.failure_count > 0 && slot.status !== 'circuit_open'"
                        class="w-full bg-amber-500 transition-all"
                        :style="{ height: errorHeightPercent(slot) }"
                      ></div>
                      <!-- Success slice (bottom, emerald) -->
                      <div
                        v-if="slot.success_count > 0 && slot.status !== 'circuit_open'"
                        class="w-full bg-emerald-500/90 group-hover:bg-emerald-500 transition-colors flex-1"
                      ></div>
                    </div>

                    <!-- Idle / Zero Stub -->
                    <div
                      v-if="slot.total_count === 0 && slot.status !== 'circuit_open'"
                      class="w-full h-1 bg-muted rounded-full group-hover:bg-muted-foreground/30 transition-colors"
                    ></div>
                  </div>
                </TooltipTrigger>
                <TooltipContent
                  side="top"
                  :side-offset="6"
                  class="flex flex-col items-start gap-1 text-xs py-2 px-3 font-mono"
                >
                  <div class="font-semibold text-background">{{ slot.label }}</div>
                  <div class="flex items-center gap-2 text-background/85">
                    <span class="text-emerald-400 dark:text-emerald-600 font-medium">✓ {{ slot.success_count }}</span>
                    <span
                      v-if="slot.failure_count > 0"
                      class="text-amber-400 dark:text-amber-600 font-medium"
                    >
                      ⚠ {{ slot.failure_count }}
                    </span>
                    <span
                      v-if="slot.blocked_count > 0"
                      class="text-red-300 dark:text-red-600 font-semibold"
                    >
                      ✕ {{ slot.blocked_count }}
                    </span>
                    <span class="text-background/60">· {{ slot.total_count }} reqs</span>
                  </div>
                  <div
                    v-if="slot.status === 'circuit_open'"
                    class="text-red-300 dark:text-red-600 font-bold"
                  >
                    🔴 {{ reasonLabel(slot.reason) }}
                  </div>
                </TooltipContent>
              </Tooltip>
            </div>
          </TooltipProvider>
        </div>

        <!-- X-Axis Timeline Labels -->
        <div class="flex items-center justify-between text-[11px] font-mono text-muted-foreground pt-1 px-1 border-t border-border/40">
          <span>{{ firstSlotLabel }}</span>
          <span class="hidden md:inline">{{ quarterSlotLabel }}</span>
          <span class="hidden sm:inline">{{ midSlotLabel }}</span>
          <span class="hidden md:inline">{{ threeQuarterSlotLabel }}</span>
          <span>{{ lastSlotLabel }}</span>
        </div>
      </CardContent>
    </Card>

    <!-- 2x2 Breakdown Cards (Failure Causes & Request Sources) -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-3.5">
      <!-- Failure Causes -->
      <Card class="shadow-xs hover:shadow-sm transition-shadow">
        <CardHeader class="pb-3 border-b border-border/50">
          <CardTitle class="text-sm font-semibold text-foreground">
            {{ t("dashboard.failure_causes") }}
          </CardTitle>
          <CardDescription class="text-xs text-muted-foreground">
            {{ t("dashboard.failure_causes_desc") }}
          </CardDescription>
        </CardHeader>
        <CardContent class="pt-4 space-y-3">
          <div
            v-for="cause in failureCauses"
            :key="cause.key"
            class="space-y-1.5 text-xs font-mono"
          >
            <div class="flex items-center justify-between text-muted-foreground">
              <span class="text-foreground font-medium">{{ cause.label }}</span>
              <span>{{ cause.count }}</span>
            </div>
            <div class="w-full bg-muted rounded-full h-1.5 overflow-hidden">
              <div
                class="h-full rounded-full transition-all duration-300"
                :class="cause.count > 0 ? causeColorClass(cause.key) : 'bg-muted'"
                :style="{ width: `${cause.percentage}%` }"
              ></div>
            </div>
          </div>
        </CardContent>
      </Card>

      <!-- Request Sources -->
      <Card class="shadow-xs hover:shadow-sm transition-shadow">
        <CardHeader class="pb-3 border-b border-border/50">
          <CardTitle class="text-sm font-semibold text-foreground">
            {{ t("dashboard.request_sources") }}
          </CardTitle>
          <CardDescription class="text-xs text-muted-foreground">
            {{ t("dashboard.request_sources_desc") }}
          </CardDescription>
        </CardHeader>
        <CardContent class="pt-4 space-y-3">
          <div
            v-for="source in requestSources"
            :key="source.key"
            class="space-y-1.5 text-xs font-mono"
          >
            <div class="flex items-center justify-between text-muted-foreground">
              <span class="text-foreground font-medium">{{ source.label }}</span>
              <span>{{ source.count }}</span>
            </div>
            <div class="w-full bg-muted rounded-full h-1.5 overflow-hidden">
              <div
                class="h-full rounded-full transition-all duration-300"
                :class="source.count > 0 ? 'bg-emerald-500' : 'bg-muted'"
                :style="{ width: `${source.percentage}%` }"
              ></div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Recent Incidents & Flight Recorder Card -->
    <Card class="shadow-xs hover:shadow-sm transition-shadow">
      <CardHeader class="pb-3 border-b border-border/50">
        <div class="flex items-center justify-between">
          <CardTitle class="text-base font-semibold text-foreground">
            {{ t("dashboard.incidents_title") }}
          </CardTitle>
          <span class="text-xs font-mono text-muted-foreground">
            {{ incidentsList.length > 0 ? `${incidentsList.length} items` : "" }}
          </span>
        </div>
      </CardHeader>

      <CardContent class="pt-4">
        <!-- Empty state -->
        <div
          v-if="incidentsList.length === 0"
          class="flex items-center gap-3 p-4 rounded-xl border border-border/40 bg-muted/20 text-xs text-muted-foreground"
        >
          <ShieldCheck class="size-4 text-muted-foreground shrink-0" />
          <span>{{ t("dashboard.incidents_empty") }}</span>
        </div>

        <!-- Incidents List -->
        <div v-else class="divide-y divide-border/50 rounded-xl border border-border/60 overflow-hidden">
          <div
            v-for="inc in incidentsList"
            :key="inc.id"
            class="flex flex-col sm:flex-row sm:items-center justify-between p-3.5 gap-2 bg-card hover:bg-muted/30 transition-colors"
          >
            <div class="flex items-start sm:items-center gap-2.5">
              <span
                class="size-2 rounded-full mt-1.5 sm:mt-0 shrink-0"
                :class="
                  inc.state === 'ongoing'
                    ? 'bg-destructive animate-pulse'
                    : inc.state === 'recovered'
                    ? 'bg-foreground'
                    : 'bg-muted-foreground'
                "
              ></span>
              <div>
                <div class="flex flex-wrap items-center gap-1.5">
                  <Badge :variant="inc.state === 'ongoing' ? 'destructive' : 'secondary'" class="text-[10px] px-2 py-0 h-4.5 font-mono">
                    {{ stateLabel(inc.state) }}
                  </Badge>
                  <Badge variant="outline" class="text-[10px] px-2 py-0 h-4.5 font-mono text-muted-foreground">
                    {{ reasonLabel(inc.reason) }}
                  </Badge>
                  <span class="text-xs font-mono text-muted-foreground">
                    {{ formatFriendlyTime(inc.started_at) }}
                  </span>
                  <span v-if="inc.duration_sec > 0" class="text-xs font-mono text-muted-foreground">
                    · {{ Math.round(inc.duration_sec) }}s
                  </span>
                </div>
                <div class="text-xs font-mono text-foreground/80 mt-1 truncate max-w-[320px] sm:max-w-[520px]">
                  {{ inc.trigger_summary || inc.trigger_path || "—" }}
                </div>
              </div>
            </div>

            <div class="flex items-center gap-3 self-end sm:self-center shrink-0">
              <span v-if="inc.blocked_count > 0" class="text-xs font-mono text-muted-foreground">
                {{ t("dashboard.shielded_requests_count", { count: inc.blocked_count }) }}
              </span>
              <Button
                variant="ghost"
                size="xs"
                class="h-7 text-xs gap-1 text-muted-foreground hover:text-foreground"
                @click="openIncident(inc.id)"
              >
                <span>{{ t("dashboard.view_postmortem") }}</span>
                <ArrowUpRight class="size-3" />
              </Button>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Post-Mortem Flight Recorder Sheet -->
    <Sheet v-model:open="s.incidentSheetOpen.value">
      <SheetContent side="right" class="w-full sm:max-w-xl md:max-w-2xl overflow-y-auto">
        <SheetHeader class="pb-3 border-b border-border/50">
          <div class="flex items-center gap-2">
            <Radio class="size-4 text-destructive" />
            <SheetTitle class="text-base font-semibold">
              {{ t("dashboard.postmortem_title") }}
            </SheetTitle>
          </div>
          <SheetDescription class="text-xs text-muted-foreground">
            {{ t("dashboard.postmortem_desc") }}
          </SheetDescription>
        </SheetHeader>

        <div v-if="currentIncident" class="py-4 space-y-5">
          <!-- Overview Status Row -->
          <div class="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
            <div class="p-3 rounded-xl border border-border/60 bg-muted/30">
              <div class="text-[10px] uppercase font-mono tracking-wider text-muted-foreground">
                {{ t("dashboard.duration") }}
              </div>
              <div class="text-sm font-bold font-mono text-foreground mt-0.5">
                {{ currentIncident.duration_sec > 0 ? `${Math.round(currentIncident.duration_sec)}s` : currentIncident.state === 'ongoing' ? t("dashboard.state_ongoing") : "—" }}
              </div>
            </div>
            <div class="p-3 rounded-xl border border-border/60 bg-muted/30">
              <div class="text-[10px] uppercase font-mono tracking-wider text-muted-foreground">
                {{ t("dashboard.blocked_shielded") }}
              </div>
              <div class="text-sm font-bold font-mono text-foreground mt-0.5">
                {{ currentIncident.blocked_count }} reqs
              </div>
            </div>
            <div class="p-3 rounded-xl border border-border/60 bg-muted/30">
              <div class="text-[10px] uppercase font-mono tracking-wider text-muted-foreground">
                Status
              </div>
              <div class="text-sm font-bold font-mono text-foreground mt-0.5">
                {{ stateLabel(currentIncident.state) }}
              </div>
            </div>
            <div class="p-3 rounded-xl border border-border/60 bg-muted/30">
              <div class="text-[10px] uppercase font-mono tracking-wider text-muted-foreground">
                Reason
              </div>
              <div class="text-sm font-bold font-mono text-foreground mt-0.5 truncate" :title="currentIncident.reason">
                {{ reasonLabel(currentIncident.reason) }}
              </div>
            </div>
          </div>

          <!-- Ground Zero Trigger Callout -->
          <div class="rounded-xl border border-destructive/30 bg-destructive/5 p-3.5 space-y-2">
            <div class="flex items-center gap-2 text-destructive font-semibold text-xs">
              <Flame class="size-4" />
              <span>{{ t("dashboard.ground_zero") }}</span>
            </div>
            <div class="font-mono text-xs bg-background/90 p-3 rounded-lg border border-border/50 text-foreground break-all">
              <div class="text-muted-foreground font-semibold">
                {{ currentIncident.trigger_trace?.method || "GET" }} {{ currentIncident.trigger_trace?.path || currentIncident.trigger_summary }}
              </div>
              <div v-if="currentIncident.trigger_error" class="text-destructive font-medium mt-1">
                Error: {{ currentIncident.trigger_error }}
              </div>
            </div>
          </div>

          <!-- Chrono-Tape Timeline -->
          <div class="space-y-4">
            <!-- 1. Traces Before -->
            <div>
              <div class="flex items-center justify-between mb-2">
                <span class="text-xs font-semibold text-foreground flex items-center gap-1.5">
                  <Clock class="size-3.5 text-muted-foreground" />
                  {{ t("dashboard.traces_before") }}
                </span>
                <span class="text-[10px] font-mono text-muted-foreground">
                  {{ currentIncident.traces_before?.length || 0 }} traces
                </span>
              </div>
              <div
                v-if="!currentIncident.traces_before || currentIncident.traces_before.length === 0"
                class="text-xs text-muted-foreground italic p-3 bg-muted/20 border border-border/40 rounded-lg"
              >
                {{ t("dashboard.no_recent_traces") }}
              </div>
              <div v-else class="space-y-1.5 max-h-48 overflow-y-auto pr-1">
                <div
                  v-for="tr in currentIncident.traces_before"
                  :key="tr.id"
                  class="flex items-center justify-between p-2.5 rounded-lg text-[11px] font-mono bg-muted/20 border border-border/40 hover:bg-muted/40 transition-colors"
                >
                  <div class="flex items-center gap-2 truncate">
                    <Badge variant="outline" class="text-[9px] px-1.5 py-0 h-4 uppercase font-semibold text-muted-foreground shrink-0">
                      {{ tr.kind || "req" }}
                    </Badge>
                    <span class="text-muted-foreground shrink-0">{{ formatTime(tr.timestamp) }}</span>
                    <span class="truncate text-foreground">{{ tr.method }} {{ tr.path }}</span>
                  </div>
                  <div class="flex items-center gap-2 shrink-0">
                    <span v-if="tr.client_ip" class="text-muted-foreground text-[10px] hidden sm:inline">{{ tr.client_ip }}</span>
                    <span :class="tr.status >= 500 ? 'text-destructive font-bold' : 'text-foreground font-medium'">
                      {{ tr.status }}
                    </span>
                  </div>
                </div>
              </div>
            </div>

            <!-- 2. Shielded During -->
            <div v-if="currentIncident.traces_during && currentIncident.traces_during.length > 0">
              <div class="flex items-center justify-between mb-2">
                <span class="text-xs font-semibold text-destructive flex items-center gap-1.5">
                  <ShieldCheck class="size-3.5" />
                  {{ t("dashboard.traces_during") }}
                </span>
                <span class="text-[10px] font-mono text-muted-foreground">
                  {{ currentIncident.traces_during.length }} sampled
                </span>
              </div>
              <div class="space-y-1.5 max-h-44 overflow-y-auto pr-1">
                <div
                  v-for="tr in currentIncident.traces_during"
                  :key="tr.id"
                  class="flex items-center justify-between p-2.5 rounded-lg text-[11px] font-mono bg-destructive/5 border border-destructive/20"
                >
                  <div class="flex items-center gap-2 truncate">
                    <Badge variant="destructive" class="text-[9px] px-1.5 py-0 h-4 uppercase font-bold shrink-0">
                      BLOCKED
                    </Badge>
                    <span class="text-muted-foreground shrink-0">{{ formatTime(tr.timestamp) }}</span>
                    <span class="truncate text-foreground">{{ tr.method }} {{ tr.path }}</span>
                  </div>
                  <span class="text-destructive font-bold shrink-0">503 FAST FAIL</span>
                </div>
              </div>
            </div>

            <!-- 3. Post-Recovery Probes -->
            <div v-if="currentIncident.traces_after && currentIncident.traces_after.length > 0">
              <div class="flex items-center justify-between mb-2">
                <span class="text-xs font-semibold text-foreground flex items-center gap-1.5">
                  <Activity class="size-3.5 text-muted-foreground" />
                  {{ t("dashboard.traces_after") }}
                </span>
                <span class="text-[10px] font-mono text-muted-foreground">
                  {{ currentIncident.traces_after.length }} traces
                </span>
              </div>
              <div class="space-y-1.5 max-h-36 overflow-y-auto pr-1">
                <div
                  v-for="tr in currentIncident.traces_after"
                  :key="tr.id"
                  class="flex items-center justify-between p-2.5 rounded-lg text-[11px] font-mono bg-muted/20 border border-border/40"
                >
                  <div class="flex items-center gap-2 truncate">
                    <Badge variant="secondary" class="text-[9px] px-1.5 py-0 h-4 uppercase font-semibold shrink-0">
                      {{ tr.kind || "probe" }}
                    </Badge>
                    <span class="text-muted-foreground shrink-0">{{ formatTime(tr.timestamp) }}</span>
                    <span class="truncate text-foreground">{{ tr.method }} {{ tr.path }}</span>
                  </div>
                  <span class="text-foreground font-bold shrink-0">{{ tr.status }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  </div>
</template>

<script setup>
import { computed, ref } from "vue";
import { useI18n } from "@/i18n";
import { useApp } from "@/state";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardAction,
  CardContent,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import {
  Activity,
  CheckCircle2,
  AlertCircle,
  ShieldCheck,
  Flame,
  RotateCcw,
  Eye,
  ArrowUpRight,
  Clock,
  Radio,
} from "@lucide/vue";

const { t } = useI18n();
const s = useApp();

const currentRange = computed(() => s.incidentRange?.value || "24h");

const lastUpdateTime = ref(new Date().toLocaleTimeString([], { hour12: false }));

const summary = computed(() => {
  return (
    s.incidents.value?.summary || {
      total_requests: 0,
      success_rate: 100,
      failures: 0,
      avg_duration_ms: 0,
    }
  );
});

const activitySlots = computed(() => {
  return s.incidents.value?.activity || [];
});

const maxBarVolume = computed(() => {
  let m = 0;
  for (const slot of activitySlots.value) {
    if (slot.total_count > m) m = slot.total_count;
  }
  return m > 0 ? m : 10;
});

const failureCauses = computed(() => {
  return s.incidents.value?.failure_causes || [];
});

const requestSources = computed(() => {
  return s.incidents.value?.request_sources || [];
});

const firstSlotLabel = computed(() => {
  return activitySlots.value[0]?.label || "—";
});

const quarterSlotLabel = computed(() => {
  const idx = Math.floor(activitySlots.value.length * 0.25);
  return activitySlots.value[idx]?.label || "—";
});

const midSlotLabel = computed(() => {
  const idx = Math.floor(activitySlots.value.length * 0.5);
  return activitySlots.value[idx]?.label || "—";
});

const threeQuarterSlotLabel = computed(() => {
  const idx = Math.floor(activitySlots.value.length * 0.75);
  return activitySlots.value[idx]?.label || "—";
});

const lastSlotLabel = computed(() => {
  return activitySlots.value[activitySlots.value.length - 1]?.label || "现在";
});

const isCircuitActive = computed(() => {
  return (
    s.status.value?.circuit?.state === "open" ||
    s.incidents.value?.active_incident?.state === "ongoing"
  );
});

const activeIncidentId = computed(() => {
  return s.incidents.value?.active_incident?.id || null;
});

const cooldownLeft = computed(() => {
  return (
    s.status.value?.circuit?.remaining ||
    s.incidents.value?.active_incident?.remaining_seconds ||
    0
  );
});

const activeReasonLabel = computed(() => {
  const r =
    s.incidents.value?.active_incident?.reason ||
    s.status.value?.circuit?.reason ||
    "";
  return reasonLabel(r);
});

const incidentsList = computed(() => {
  return s.incidents.value?.history || [];
});

const currentIncident = computed(() => {
  return s.selectedIncident.value || null;
});

function barHeightPercent(slot) {
  if (slot.status === "circuit_open") return "100%";
  if (slot.total_count === 0) return "0%";
  const ratio = slot.total_count / maxBarVolume.value;
  const pct = Math.max(6, Math.min(100, Math.round(ratio * 100)));
  return `${pct}%`;
}

function blockedHeightPercent(slot) {
  if (slot.status === "circuit_open") return "100%";
  if (slot.total_count === 0 || slot.blocked_count === 0) return "0%";
  const ratio = slot.blocked_count / slot.total_count;
  return `${Math.max(10, Math.round(ratio * 100))}%`;
}

function errorHeightPercent(slot) {
  if (slot.status === "circuit_open") return "0%";
  if (slot.total_count === 0 || slot.failure_count === 0) return "0%";
  const ratio = slot.failure_count / slot.total_count;
  return `${Math.max(10, Math.round(ratio * 100))}%`;
}

function causeColorClass(key) {
  if (key === "banned" || key === "exceed" || key === "shielded") return "bg-destructive";
  if (key === "timeout" || key === "upstream_error") return "bg-amber-500";
  return "bg-destructive";
}

function selectRange(range) {
  s.loadIncidents(range);
  lastUpdateTime.value = new Date().toLocaleTimeString([], { hour12: false });
}

function refreshData() {
  s.loadIncidents(currentRange.value);
  lastUpdateTime.value = new Date().toLocaleTimeString([], { hour12: false });
}

function stateLabel(state) {
  if (state === "ongoing") return t("dashboard.state_ongoing");
  if (state === "recovered") return t("dashboard.state_recovered");
  if (state === "reset") return t("dashboard.state_reset");
  return state || "—";
}

function reasonLabel(reason) {
  if (!reason) return "—";
  if (reason === "banned") return t("dashboard.reason_banned");
  if (reason === "exceed") return t("dashboard.reason_exceed");
  if (reason === "upstream_502") return t("dashboard.reason_upstream_502");
  if (reason === "upstream_503") return t("dashboard.reason_upstream_503");
  if (reason === "upstream_429") return t("dashboard.reason_upstream_429");
  if (reason === "archiver_unavailable" || reason === "upstream_403") return t("dashboard.reason_archiver_unavailable");
  if (reason === "timeout") return t("dashboard.reason_timeout") || "网络超时";
  return reason;
}

function formatTime(isoStr) {
  if (!isoStr) return "";
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return isoStr;
  return d.toLocaleTimeString([], { hour12: false });
}

function formatFriendlyTime(isoStr) {
  if (!isoStr) return "";
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return isoStr;

  const now = new Date();
  const isToday =
    d.getDate() === now.getDate() &&
    d.getMonth() === now.getMonth() &&
    d.getFullYear() === now.getFullYear();

  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  const isYesterday =
    d.getDate() === yesterday.getDate() &&
    d.getMonth() === yesterday.getMonth() &&
    d.getFullYear() === yesterday.getFullYear();

  const timeStr = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });

  if (isToday) {
    return `${t("dashboard.today") || "今天"} ${timeStr}`;
  }
  if (isYesterday) {
    return `${t("dashboard.yesterday") || "昨天"} ${timeStr}`;
  }
  const monthDay = `${d.getMonth() + 1}-${d.getDate()}`;
  return `${monthDay} ${timeStr}`;
}

function openIncident(id) {
  s.inspectIncident(id);
}

function confirmReset() {
  s.resetCircuit();
}
</script>
