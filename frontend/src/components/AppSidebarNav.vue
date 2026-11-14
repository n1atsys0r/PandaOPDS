<script setup>
import {
  Package2Icon as Package2,
  LayoutDashboardIcon as LayoutDashboard,
  CompassIcon as Compass,
  SettingsIcon as Settings,
} from "@lucide/vue";
import {
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
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
    <SidebarGroup>
      <SidebarGroupLabel>运行</SidebarGroupLabel>
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
    <SidebarGroup>
      <SidebarGroupLabel>配置</SidebarGroupLabel>
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
