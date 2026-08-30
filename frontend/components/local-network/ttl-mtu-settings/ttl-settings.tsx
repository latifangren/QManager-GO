import TTLSettingsCard from "./ttl-settings-card";
import MTUSettingsCard from "./mtu-settings-card";

const TTLandMTUSettingsComponent = () => {
  return (
    <div className="@container/main mx-auto p-2">
      <div className="mb-6">
        <h1 className="text-3xl font-bold mb-2">TTL and MTU Settings</h1>
        <p className="text-muted-foreground">
          Set custom Time To Live, Hop Limit, and Maximum Transmission Unit
          values on the cellular data interface.
        </p>
      </div>
      <div className="grid grid-cols-1 @3xl/main:grid-cols-2 grid-flow-row gap-4">
        <TTLSettingsCard />
        <MTUSettingsCard />
      </div>
    </div>
  );
};

export default TTLandMTUSettingsComponent;
