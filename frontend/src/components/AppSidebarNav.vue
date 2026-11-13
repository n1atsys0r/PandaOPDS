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
              @click="navigate('dashboard')"
            >
              <LayoutDashboard /><span>概览</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'explore'"
              @click="navigate('explore')"
            >
              <Compass /><span>主页</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'archive'"
              @click="navigate('archive')"
            >
              <Package2 /><span>归档</span>
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
              @click="navigate('config')"
            >
              <Settings /><span>设置</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  </SidebarContent>
</template>
