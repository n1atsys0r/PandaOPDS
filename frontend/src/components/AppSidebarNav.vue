<script setup>
import {
  Package2Icon as Package2,
  LayoutDashboardIcon as LayoutDashboard,
  PanelsTopLeftIcon as PanelsTopLeft,
  CompassIcon as Compass,
  SettingsIcon as Settings,
} from "@lucide/vue";
import {
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";

const props = defineProps({ page: { type: String, required: true } });
const emit = defineEmits(["navigate"]);
const { setOpenMobile } = useSidebar();

function navigate(page) {
  emit("navigate", page);
  setOpenMobile(false);
}
</script>

<template>
  <SidebarContent>
    <!-- 主导航功能区 -->
    <SidebarGroup>
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'dashboard'"
              tooltip="概览"
              @click="navigate('dashboard')"
            >
              <LayoutDashboard /><span class="group-data-[collapsible=icon]:hidden">概览</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'home'"
              tooltip="编排"
              @click="navigate('home')"
            >
              <PanelsTopLeft /><span class="group-data-[collapsible=icon]:hidden">编排</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'explore'"
              tooltip="探索"
              @click="navigate('explore')"
            >
              <Compass /><span class="group-data-[collapsible=icon]:hidden">探索</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'archive'"
              tooltip="归档"
              @click="navigate('archive')"
            >
              <Package2 /><span class="group-data-[collapsible=icon]:hidden">归档</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>

    <!-- 置底工具区 -->
    <SidebarGroup class="mt-auto">
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'config'"
              tooltip="设置"
              @click="navigate('config')"
            >
              <Settings /><span class="group-data-[collapsible=icon]:hidden">设置</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  </SidebarContent>
</template>
