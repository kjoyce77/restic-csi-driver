### CSI Plugin Functionality Overview

**Configuration:**
- The plugin will be configured with:
  - The name of a thin LVM pool for volume management.
  - Restic destinations for backup operations.

**On Volume Publish:**
1. **Volume Creation:**
   - If a thin volume with the specified name doesn't exist, it will be created.

2. **Backup Handling:**
   - The plugin will check the Restic destinations for recent backups.
   - If multiple destinations have backups from the same time, the closest one will be selected.
   - If no backups are found, the next steps are skipped.

3. **Backup Restoration:**
   - Mount the thin volume at a standard mount point (e.g., `/mnt/volumes`).
   - Mount the Restic snapshot at a designated snapshot mount point (`/mnt/restic`).
   - Perform an `rsync -A` operation from the snapshot to the volume.
   - Unmount both the volume and the Restic snapshot.

4. **Final Mounting:**
   - The volume is then mounted at the destination specified by the orchestration engine.

**On Volume Unpublish:**
1. **Preparation:**
   - Record the current time.
   - Unmount the volume from its published mount point and remount it to a configured staging mount point.

2. **Backup Operation:**
   - Initiate a pool of worker threads to back up the mounted volume to the Restic destinations.
   - Use the recorded time for all backups to ensure uniformity for future restore operations.
   - Once all backups are completed by the threads, unmount the volume and conclude the operation.

**Future Scope:**
- Incorporate live snapshot functionalities in subsequent versions.
